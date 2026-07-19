package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/gobicycle/bicycle/alerts"
	"github.com/gobicycle/bicycle/audit"
	"github.com/gobicycle/bicycle/config"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
	log "github.com/sirupsen/logrus"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"math/big"
	"sync"
	"sync/atomic"
	"time"
)

type WithdrawalsProcessor struct {
	db               storage
	bc               blockchain
	wallets          Wallets
	coldWallet       *address.Address
	wg               *sync.WaitGroup
	gracefulShutdown atomic.Bool
	alerter          *alerts.Publisher
	// lastStuckAlert debounces the stuck-withdrawal alert (rounds run every 80s;
	// without it the alert would fire every round while the hot wallet is dry).
	// Touched only by the single external-withdrawals goroutine.
	lastStuckAlert time.Time
}

// stuckWithdrawal is one external withdrawal skipped this round because the hot
// wallet balance does not cover it.
type stuckWithdrawal struct {
	QueryID  int64
	Currency string
	Need     *big.Int
	Have     *big.Int
}

type internalWithdrawal struct {
	Memo uuid.UUID
	Task InternalWithdrawalTask
}

type serviceWithdrawal struct {
	TonAmount Coins
	Filled    bool
	Task      ServiceWithdrawalTask
}

type withdrawals struct {
	Messages []*wallet.Message
	External []ExternalWithdrawalTask
	Internal []internalWithdrawal
	Service  []serviceWithdrawal
}

func NewWithdrawalsProcessor(
	wg *sync.WaitGroup,
	db storage,
	bc blockchain,
	wallets Wallets,
	coldWallet *address.Address,
	alerter *alerts.Publisher,
) *WithdrawalsProcessor {
	w := &WithdrawalsProcessor{
		db:         db,
		bc:         bc,
		wallets:    wallets,
		coldWallet: coldWallet,
		wg:         wg,
		alerter:    alerter,
	}
	return w
}

func (p *WithdrawalsProcessor) Start() {
	p.wg.Add(3)
	go p.startWithdrawalsProcessor()
	go p.startInternalTonWithdrawalsProcessor()
	go p.startExpirationProcessor()
}

func (p *WithdrawalsProcessor) Stop() {
	p.gracefulShutdown.Store(true)
}

func (p *WithdrawalsProcessor) startWithdrawalsProcessor() {
	defer p.wg.Done()
	log.Infof("External withdrawal processor started")
	for {
		p.waitSync() // gracefulShutdown break must be after waitSync
		if p.gracefulShutdown.Load() {
			log.Infof("External withdrawal processor stopped")
			break
		}
		time.Sleep(config.ExternalWithdrawalPeriod)
		// Kill-switch (F2/M6): after a DB restore the processing/processed flags may
		// have been rolled back, so broadcasting would re-send already-settled
		// withdrawals (double payout). An operator sets EXTERNAL_WITHDRAWALS_ENABLED
		// false to halt broadcasting until withdrawal state is reconciled with chain.
		if !config.Config.ExternalWithdrawalsEnabled {
			log.Warnf("external withdrawals disabled (EXTERNAL_WITHDRAWALS_ENABLED=false), skipping round")
			continue
		}
		// A transient DB/RPC error must not crash the whole processor: log and
		// retry next round. Withdrawals persisted this round carry a TTL, so an
		// interrupted round is recovered by the expiration processor.
		if err := p.processExternalWithdrawalsRound(); err != nil {
			log.Errorf("external withdrawals round error: %v", err)
		}
	}
}

func (p *WithdrawalsProcessor) processExternalWithdrawalsRound() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*25) // must be < ExternalWithdrawalPeriod
	defer cancel()

	if err := p.makeColdWalletWithdrawals(ctx); err != nil {
		return fmt.Errorf("make withdrawals to cold wallet: %w", err)
	}
	w, err := p.buildWithdrawalMessages(ctx)
	if err != nil {
		return fmt.Errorf("make withdrawal messages: %w", err)
	}
	if len(w.Messages) == 0 {
		return nil
	}
	extMsg, err := p.wallets.TonHotWallet.BuildExternalMessageForMany(ctx, w.Messages)
	if err != nil {
		return fmt.Errorf("build hotwallet external msg: %w", err)
	}
	info, err := getHighLoadWalletExtMsgInfo(extMsg)
	if err != nil {
		return fmt.Errorf("get external message uuid: %w", err)
	}
	if err := p.db.CreateExternalWithdrawals(ctx, w.External, info.UUID, info.TTL); err != nil {
		return fmt.Errorf("save external withdrawals: %w", err)
	}
	for _, sw := range w.Service {
		if err := p.db.UpdateServiceWithdrawalRequest(ctx, sw.Task, sw.TonAmount, info.TTL, sw.Filled); err != nil {
			return fmt.Errorf("update service withdrawal: %w", err)
		}
	}
	for _, iw := range w.Internal {
		if err := p.db.SaveInternalWithdrawalTask(ctx, iw.Task, info.TTL, iw.Memo); err != nil {
			return fmt.Errorf("save internal withdrawal: %w", err)
		}
	}
	if err := p.bc.SendExternalMessage(ctx, extMsg); err != nil {
		log.Errorf("send external msg error: %v", err)
	}
	return nil
}

func (p *WithdrawalsProcessor) buildWithdrawalMessages(ctx context.Context) (withdrawals, error) {
	var (
		usedAddresses []Address
		res           withdrawals
		stuck         []stuckWithdrawal
	)

	balances, err := p.getHotWalletBalances(ctx)
	if err != nil {
		return withdrawals{}, fmt.Errorf("get hot wallet balance error: %s", err.Error())
	}

	serviceTasks, err := p.db.GetServiceHotWithdrawalTasks(ctx, 250)
	if err != nil {
		return withdrawals{}, err
	}
	for _, t := range serviceTasks {
		if decreaseBalances(balances, TonSymbol, config.JettonTransferTonAmount.Nano()) {
			continue
		}
		msg, w, err := p.buildServiceWithdrawalMessage(ctx, t)
		if err != nil {
			return withdrawals{}, err
		}
		if len(msg) != 0 {
			// block scanner determines the uniqueness of the message in the batch by the dest address
			// the dest address will be the address of the proxy contract
			// TON deposit address is the dest addr for TON deposit filling message
			// so the address `t.From` is the dest address when checking the uniqueness
			usedAddresses = append(usedAddresses, t.From)
			res.Messages = append(res.Messages, msg...)
			res.Service = append(res.Service, w)
		} else {
			// save rejected service withdrawals
			err = p.db.UpdateServiceWithdrawalRequest(ctx, w.Task, w.TonAmount, time.Now(), w.Filled)
			if err != nil {
				return withdrawals{}, err
			}
		}
	}

	// `internalTask.From` address is the address of deposit Jetton wallet
	// the dest address for uniqueness check is proxy contract address
	// so the proxy contract address must be deduplicated with usedAddresses in db query
	internalTasks, err := p.db.GetJettonInternalWithdrawalTasks(ctx, usedAddresses, 250)
	if err != nil {
		return withdrawals{}, err
	}
	for _, t := range internalTasks {
		if len(res.Messages) > 250 {
			break
		}
		if decreaseBalances(balances, TonSymbol, config.JettonTransferTonAmount.Nano()) {
			continue
		}
		msg, memo, err := p.buildJettonInternalWithdrawalMessage(ctx, t)
		if err != nil {
			return withdrawals{}, err
		}
		if len(msg) != 0 {
			res.Messages = append(res.Messages, msg...)
			res.Internal = append(res.Internal, internalWithdrawal{
				Task: t,
				Memo: memo,
			})
		}
	}

	// not filter usedAddresses by DB and perform internal addresses checking and logging
	externalTasks, err := p.db.GetExternalWithdrawalTasks(ctx, 250)
	if err != nil {
		return withdrawals{}, err
	}
	for _, w := range externalTasks {
		if len(res.Messages) > 250 {
			break
		}
		t, ok := p.db.GetWalletType(w.Destination)
		if ok {
			audit.Log(audit.Warning, string(TonHotWallet), ExternalWithdrawalEvent,
				fmt.Sprintf("withdrawal task to internal %s address %s", t, w.Destination.ToUserFormat()))
			continue
		}
		// Build the message BEFORE reserving balance so a malformed task (e.g. bad
		// binary comment) is skipped without needing to roll back the reservation,
		// and — crucially — without crashing the whole withdrawal round. A later
		// round retries; funds are neither sent nor lost here.
		msg, err := p.buildExternalWithdrawalMessage(w)
		if err != nil {
			log.Errorf("build external withdrawal message (query_id %d): %v, skipping", w.QueryID, err)
			continue
		}
		if decreaseBalances(balances, w.Currency, w.Amount.BigInt()) {
			// Hot wallet lacks enough of this currency (or enough TON for jetton gas)
			// to cover the withdrawal. It is silently retried every round, which looks
			// like a wedged withdrawal — log why so the cause (fund the hot wallet) is
			// visible instead of a silent skip. For a jetton, "have" is the jetton
			// balance; sending it also needs TON gas in the hot wallet.
			have := big.NewInt(0)
			if b, ok := balances[w.Currency]; ok && b != nil {
				have = new(big.Int).Set(b) // copy: balances map keeps mutating this round
			}
			log.Warnf("skipping withdrawal query_id %d: insufficient hot wallet %s (need %s, have %s) — fund the hot wallet",
				w.QueryID, w.Currency, w.Amount.String(), have.String())
			stuck = append(stuck, stuckWithdrawal{
				QueryID:  w.QueryID,
				Currency: w.Currency,
				Need:     w.Amount.BigInt(),
				Have:     have,
			})
			continue
		}
		res.Messages = append(res.Messages, msg)
		res.External = append(res.External, w)
	}
	p.maybeAlertStuckWithdrawals(stuck)
	return res, nil
}

func (p *WithdrawalsProcessor) getHotWalletBalances(ctx context.Context) (map[string]*big.Int, error) {
	res := make(map[string]*big.Int)
	balance, _, err := p.bc.GetAccountCurrentState(ctx, p.wallets.TonHotWallet.Address())
	if err != nil {
		return nil, err
	}
	res[TonSymbol] = balance
	for cur, w := range p.wallets.JettonHotWallets {
		balance, err := p.bc.GetLastJettonBalance(ctx, w.Address)
		if err != nil {
			return nil, err
		}
		res[cur] = balance
	}
	return res, nil
}

// decreaseBalances returns true if balance < amount
func decreaseBalances(balances map[string]*big.Int, currency string, amount *big.Int) bool {
	if currency == TonSymbol {
		if balances[TonSymbol].Cmp(amount) == -1 { // balance < amount
			return true
		}
		balances[TonSymbol].Sub(balances[TonSymbol], amount)
		return false
	}
	if balances[currency].Cmp(amount) == -1 || // balance < amount
		balances[TonSymbol].Cmp(config.JettonTransferTonAmount.Nano()) == -1 { // balance < JettonTransferTonAmount
		return true
	}
	balances[currency].Sub(balances[currency], amount)
	balances[TonSymbol].Sub(balances[TonSymbol], config.JettonTransferTonAmount.Nano())
	return false
}

// stuckWithdrawalAlertPeriod is how often the stuck-withdrawal alert repeats
// while withdrawals stay unfunded (first occurrence alerts immediately).
const stuckWithdrawalAlertPeriod = time.Hour

// alertDecimals maps currency to on-chain decimals for human-readable alert
// amounts. ponytail: hardcoded for the currencies we run; unknown jettons fall
// back to raw units.
var alertDecimals = map[string]int32{TonSymbol: 9, "USDT": 6}

func fmtAlertAmount(currency string, v *big.Int) string {
	if d, ok := alertDecimals[currency]; ok {
		return decimal.NewFromBigInt(v, -d).String() + " " + currency
	}
	return v.String() + " " + currency + " (raw)"
}

// maybeAlertStuckWithdrawals sends a Telegram alert (via the shared alerts
// stream) when external withdrawals were skipped for lack of hot wallet funds.
// Debounced to one alert per stuckWithdrawalAlertPeriod. The summary of
// un-swept deposits needs extra chain reads, so it runs in a goroutine with its
// own deadline instead of eating the 25s round budget.
func (p *WithdrawalsProcessor) maybeAlertStuckWithdrawals(stuck []stuckWithdrawal) {
	if len(stuck) == 0 || !p.alerter.Configured() {
		return
	}
	if time.Since(p.lastStuckAlert) < stuckWithdrawalAlertPeriod {
		return
	}
	p.lastStuckAlert = time.Now()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
		defer cancel()
		if err := p.alerter.Send(ctx, alerts.LevelWarn, p.buildStuckAlertText(ctx, stuck)); err != nil {
			log.Errorf("send stuck withdrawal alert: %v", err)
		}
	}()
}

func (p *WithdrawalsProcessor) buildStuckAlertText(ctx context.Context, stuck []stuckWithdrawal) string {
	return renderStuckAlert(stuck, p.pendingSweepsSummary(ctx))
}

// renderStuckAlert is pure so the alert text is unit-testable without mocking
// chain and DB (sweeps is the pendingSweepsSummary output).
func renderStuckAlert(stuck []stuckWithdrawal, sweeps string) string {
	var b strings.Builder
	b.WriteString("⏳ Вывод завис: не хватает средств на горячем кошельке\n")
	for _, s := range stuck {
		b.WriteString(fmt.Sprintf("• query_id %d: нужно %s, на hot %s",
			s.QueryID, fmtAlertAmount(s.Currency, s.Need), fmtAlertAmount(s.Currency, s.Have)))
		// decreaseBalances also fails a jetton withdrawal when the wallet holds the
		// jetton but not the TON gas — "have >= need" would read as nonsense without
		// naming the real cause.
		if s.Currency != TonSymbol && s.Have.Cmp(s.Need) >= 0 {
			b.WriteString(" (не хватает TON на газ)")
		}
		b.WriteString("\n")
	}
	b.WriteString(sweeps)
	b.WriteString("→ пополните горячий кошелёк или дождитесь свипа депозитов")
	return b.String()
}

// pendingSweepsSummary describes deposit funds that have not reached the hot
// wallet yet, split by the sweep cutoff: sums above it arrive on their own
// within minutes, sums below it stay on deposits until topped up — different
// operator actions. Best-effort: chain/DB errors just shrink the summary.
func (p *WithdrawalsProcessor) pendingSweepsSummary(ctx context.Context) string {
	type bucket struct {
		above, below   *big.Int
		aboveN, belowN int
		cutoff         *big.Int
	}
	buckets := make(map[string]*bucket)
	add := func(cur string, balance, cutoff *big.Int) {
		bk, ok := buckets[cur]
		if !ok {
			bk = &bucket{above: big.NewInt(0), below: big.NewInt(0), cutoff: cutoff}
			buckets[cur] = bk
		}
		if balance.Cmp(cutoff) == 1 {
			bk.above.Add(bk.above, balance)
			bk.aboveN++
		} else {
			bk.below.Add(bk.below, balance)
			bk.belowN++
		}
	}

	tonTasks, err := p.db.GetTonInternalWithdrawalTasks(ctx, 40)
	if err != nil {
		log.Errorf("stuck alert: get TON internal withdrawal tasks: %v", err)
	}
	for _, t := range tonTasks {
		balance, state, err := p.bc.GetAccountCurrentState(ctx, t.From.ToTonutilsAddressStd(0))
		if err != nil || state == tlb.AccountStatusNonExist || balance.Sign() != 1 {
			continue
		}
		add(TonSymbol, balance, config.Config.Ton.Withdrawal)
	}

	jettonTasks, err := p.db.GetJettonInternalWithdrawalTasks(ctx, nil, 40)
	if err != nil {
		log.Errorf("stuck alert: get jetton internal withdrawal tasks: %v", err)
	}
	for _, t := range jettonTasks {
		j, ok := config.Config.Jettons[t.Currency]
		if !ok {
			continue
		}
		balance, err := p.bc.GetLastJettonBalance(ctx, t.From.ToTonutilsAddressStd(0))
		if err != nil || balance.Sign() != 1 {
			continue
		}
		add(t.Currency, balance, j.WithdrawalCutoff)
	}

	if len(buckets) == 0 {
		return "Несвипнутых депозитов нет — средства нужно завести на горячий кошелёк извне.\n"
	}
	var b strings.Builder
	for cur, bk := range buckets {
		if bk.aboveN > 0 {
			b.WriteString(fmt.Sprintf("Ожидает свипа на hot (придёт само): %s на %d депозитах\n",
				fmtAlertAmount(cur, bk.above), bk.aboveN))
		}
		if bk.belowN > 0 {
			b.WriteString(fmt.Sprintf("Ниже cutoff %s (само НЕ придёт): %s на %d депозитах\n",
				fmtAlertAmount(cur, bk.cutoff), fmtAlertAmount(cur, bk.below), bk.belowN))
		}
	}
	return b.String()
}

func (p *WithdrawalsProcessor) buildJettonInternalWithdrawalMessage(
	ctx context.Context,
	task InternalWithdrawalTask,
) (
	[]*wallet.Message,
	uuid.UUID,
	error,
) {
	proxy, err := NewJettonProxy(task.SubwalletID, p.wallets.TonHotWallet.Address())
	if err != nil {
		return nil, uuid.UUID{}, err
	}
	jettonWalletAddress := task.From.ToTonutilsAddressStd(0)
	balance, err := p.bc.GetLastJettonBalance(ctx, jettonWalletAddress)
	if err != nil {
		return nil, uuid.UUID{}, err
	}
	if balance.Cmp(config.Config.Jettons[task.Currency].WithdrawalCutoff) == 1 { // balance > MinimalJettonWithdrawalAmount
		memo, err := uuid.NewV4()
		if err != nil {
			return nil, uuid.UUID{}, err
		}
		msg, err := BuildJettonProxyWithdrawalMessage(
			*proxy,
			jettonWalletAddress,
			p.wallets.TonHotWallet.Address(),
			config.JettonInternalForwardAmount,
			balance,
			memo.String(),
		)
		if err != nil {
			return nil, uuid.UUID{}, err
		}
		return []*wallet.Message{msg}, memo, nil
	}
	return []*wallet.Message{}, uuid.UUID{}, nil
}

func (p *WithdrawalsProcessor) buildServiceWithdrawalMessage(
	ctx context.Context,
	task ServiceWithdrawalTask,
) (
	[]*wallet.Message,
	serviceWithdrawal,
	error,
) {
	t, ok := p.db.GetWalletType(task.From)
	if !ok || !(t == JettonOwner || t == TonDepositWallet) {
		return nil, serviceWithdrawal{}, fmt.Errorf("invalid service withdrawal address")
	}
	if t == TonDepositWallet { // only fill TON deposit to send Jetton transfer message later
		return p.buildServiceFilling(ctx, task)
	}

	if task.JettonMaster == nil { // full TON withdrawal from Jetton proxy
		return p.buildServiceTonWithdrawal(ctx, task)
	}
	// Jetton withdrawal from Jetton wallet
	return p.buildServiceJettonWithdrawal(ctx, task)
}

func (p *WithdrawalsProcessor) buildServiceFilling(
	ctx context.Context,
	task ServiceWithdrawalTask,
) (
	[]*wallet.Message,
	serviceWithdrawal,
	error,
) {
	deposit := task.From.ToTonutilsAddressStd(0)

	jettonWallet, err := p.bc.GetJettonWalletAddress(
		ctx,
		deposit,
		task.JettonMaster.ToTonutilsAddressStd(0))
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	jettonBalance, err := p.bc.GetLastJettonBalance(ctx, jettonWallet)
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}

	if jettonBalance.Cmp(big.NewInt(0)) == 0 {
		audit.Log(audit.Warning, string(TonDepositWallet), ServiceWithdrawalEvent,
			fmt.Sprintf("zero balance of Jettons %s on TON deposit address %s",
				task.JettonMaster.ToTonutilsAddressStd(0).String(),
				TonutilsAddressToUserFormat(deposit)))
		return nil, serviceWithdrawal{
			TonAmount: ZeroCoins(),
			Task:      task,
		}, nil
	}
	msg, err := buildTonFillMessage(deposit, config.JettonTransferTonAmount, task.Memo)
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	task.JettonAmount = NewCoins(jettonBalance)
	return []*wallet.Message{msg}, serviceWithdrawal{
		TonAmount: ZeroCoins(),
		Task:      task,
		Filled:    true,
	}, nil
}

func (p *WithdrawalsProcessor) buildServiceTonWithdrawal(
	ctx context.Context,
	task ServiceWithdrawalTask,
) (
	[]*wallet.Message,
	serviceWithdrawal,
	error,
) {
	proxy, err := NewJettonProxy(task.SubwalletID, p.wallets.TonHotWallet.Address())
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	tonBalance, _, err := p.bc.GetAccountCurrentState(ctx, proxy.address)
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	res := serviceWithdrawal{
		TonAmount: NewCoins(tonBalance),
		Task:      task,
	}
	if tonBalance.Cmp(big.NewInt(0)) == 0 {
		audit.Log(audit.Warning, string(JettonOwner), ServiceWithdrawalEvent,
			fmt.Sprintf("zero balance of TONs on proxy address %s", TonutilsAddressToUserFormat(proxy.address)))
		return nil, res, nil
	}
	msg, err := buildJettonProxyServiceTonWithdrawalMessage(*proxy, p.wallets.TonHotWallet.Address(), task.Memo)
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	return []*wallet.Message{msg}, res, nil
}

func (p *WithdrawalsProcessor) buildServiceJettonWithdrawal(
	ctx context.Context,
	task ServiceWithdrawalTask,
) (
	[]*wallet.Message,
	serviceWithdrawal,
	error,
) {
	proxy, err := NewJettonProxy(task.SubwalletID, p.wallets.TonHotWallet.Address())
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	jettonWallet, err := p.bc.GetJettonWalletAddress(ctx, proxy.address, task.JettonMaster.ToTonutilsAddressStd(0))
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	t, ok := p.db.GetWalletTypeByTonutilsAddress(jettonWallet)
	if ok {
		audit.Log(audit.Warning, string(JettonOwner), ServiceWithdrawalEvent,
			fmt.Sprintf("service withdrawal from known internal %s address %s rejected",
				t, TonutilsAddressToUserFormat(jettonWallet)))
		return nil, serviceWithdrawal{
			TonAmount: ZeroCoins(),
			Task:      task,
		}, nil
	}

	jettonBalance, err := p.bc.GetLastJettonBalance(ctx, jettonWallet)
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}

	if jettonBalance.Cmp(big.NewInt(0)) == 0 {
		audit.Log(audit.Warning, string(JettonOwner), ServiceWithdrawalEvent,
			fmt.Sprintf("zero %s Jetton balance on proxy address %s",
				task.JettonMaster.ToTonutilsAddressStd(0).String(),
				TonutilsAddressToUserFormat(proxy.address)))
		return nil, serviceWithdrawal{
			TonAmount: ZeroCoins(),
			Task:      task,
		}, nil
	}
	task.JettonAmount = NewCoins(jettonBalance)
	res := serviceWithdrawal{
		TonAmount: ZeroCoins(),
		Task:      task,
	}

	msg, err := BuildJettonProxyWithdrawalMessage(
		*proxy,
		jettonWallet,
		p.wallets.TonHotWallet.Address(),
		tlb.FromNanoTONU(0), // zero forward amount to prevent notification sending and incorrect internal income invoking
		jettonBalance,
		task.Memo.String(),
	)
	if err != nil {
		return nil, serviceWithdrawal{}, err
	}
	return []*wallet.Message{msg}, res, nil
}

func (p *WithdrawalsProcessor) buildExternalWithdrawalMessage(wt ExternalWithdrawalTask) (*wallet.Message, error) {
	if wt.Currency == TonSymbol {
		return BuildTonWithdrawalMessage(wt)
	}
	jw := p.wallets.JettonHotWallets[wt.Currency]
	return BuildJettonWithdrawalMessage(wt, p.wallets.TonHotWallet, jw.Address)
}

func (p *WithdrawalsProcessor) startExpirationProcessor() {
	log.Infof("Expiration processor started")
	defer p.wg.Done()
	for {
		p.waitSync() // gracefulShutdown break must be after waitSync
		if p.gracefulShutdown.Load() {
			log.Infof("Expiration processor stopped")
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3) // must be < ExpirationProcessorPeriod
		err := p.db.SetExpired(ctx)
		if err != nil {
			log.Errorf("set expired withdrawals error: %v", err)
		}
		cancel()
		time.Sleep(config.ExpirationProcessorPeriod)
	}
}

func (p *WithdrawalsProcessor) startInternalTonWithdrawalsProcessor() {
	defer p.wg.Done()
	log.Infof("Internal TON withdrawal processor started")
	for {
		p.waitSync() // gracefulShutdown break must be after waitSync
		if p.gracefulShutdown.Load() {
			log.Infof("Internal TON withdrawal processor stopped")
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*120) // TODO: split context
		serviceTasks, err := p.db.GetServiceDepositWithdrawalTasks(ctx, 5)
		if err != nil {
			log.Errorf("get service withdrawal tasks error: %v", err)
		}
		for _, task := range serviceTasks {
			err = p.serviceWithdrawJettons(ctx, task)
			if err != nil {
				log.Errorf("Jettons service internal withdrawal error: %v", err)
			}
			time.Sleep(time.Millisecond * 50)
		}

		internalTasks, err := p.db.GetTonInternalWithdrawalTasks(ctx, 40) // context limitation
		if err != nil {
			log.Errorf("get internal withdrawal tasks error: %v", err)
		}
		for _, task := range internalTasks {
			err = p.withdrawTONsFromDeposit(ctx, task)
			if err != nil {
				log.Errorf("TONs internal withdrawal error: %v", err)
			}
			time.Sleep(time.Millisecond * 50)
		}
		cancel()
		time.Sleep(config.InternalWithdrawalPeriod)
	}
}

func (p *WithdrawalsProcessor) withdrawTONsFromDeposit(ctx context.Context, task InternalWithdrawalTask) error {
	subwallet, err := p.wallets.TonBasicWallet.GetSubwallet(task.SubwalletID)
	if err != nil {
		return err
	}
	spec := subwallet.GetSpec().(*wallet.SpecV3)
	spec.SetMessagesTTL(uint32(config.ExternalMessageLifetime.Seconds()))

	balance, state, err := p.bc.GetAccountCurrentState(ctx, subwallet.Address())
	if err != nil {
		return err
	}
	if state == tlb.AccountStatusNonExist {
		return nil
	}
	if balance.Cmp(config.Config.Ton.Withdrawal) == 1 { // Balance > MinimalTonWithdrawalAmount
		memo, err := uuid.NewV4()
		if err != nil {
			return err
		}
		err = p.db.SaveInternalWithdrawalTask(ctx, task, time.Now().Add(config.ExternalMessageLifetime), memo)
		if err != nil {
			return err
		}
		// time.Now().Add(config.ExternalMessageLifetime) and real TTL
		// should be very close since the withdrawal occurs immediately
		err = WithdrawTONs(ctx, subwallet, p.wallets.TonHotWallet, memo.String())
		if err != nil {
			audit.Log(audit.Info, string(TonDepositWallet), InternalWithdrawalEvent,
				fmt.Sprintf("TONs internal withdrawal from deposit %s error: %s",
					task.From.ToUserFormat(), err.Error()))
		}
	}
	return nil
}

func (p *WithdrawalsProcessor) serviceWithdrawJettons(ctx context.Context, task ServiceWithdrawalTask) error {
	subwallet, err := p.wallets.TonBasicWallet.GetSubwallet(task.SubwalletID)
	if err != nil {
		return err
	}
	spec := subwallet.GetSpec().(*wallet.SpecV3)
	spec.SetMessagesTTL(uint32(config.ExternalMessageLifetime.Seconds()))

	_, state, err := p.bc.GetAccountCurrentState(ctx, subwallet.Address())
	if err != nil {
		return err
	}
	if state == tlb.AccountStatusNonExist {
		return nil
	}

	jettonWallet, err := p.bc.GetJettonWalletAddress(ctx, subwallet.Address(), task.JettonMaster.ToTonutilsAddressStd(0))
	if err != nil {
		return err
	}

	err = p.db.UpdateServiceWithdrawalRequest(ctx, task, ZeroCoins(),
		time.Now().Add(config.ExternalMessageLifetime), false)
	if err != nil {
		return err
	}
	// time.Now().Add(config.ExternalMessageLifetime) and real TTL
	// should be very close since the withdrawal occurs immediately
	err = WithdrawJettons(ctx, subwallet, p.wallets.TonHotWallet, jettonWallet, tlb.FromNanoTONU(0),
		task.JettonAmount, task.Memo.String()) // zero forward TON amount to prevent notify message invoking
	if err != nil {
		log.Errorf("Jettons service withdrawal error: %v", err)
		audit.Log(audit.Info, string(TonDepositWallet), ServiceWithdrawalEvent,
			fmt.Sprintf("Jettons service withdrawal from deposit %s error: %s",
				task.From.ToUserFormat(), err.Error()))
	}
	return nil
}

func (p *WithdrawalsProcessor) waitSync() {
	for {
		if p.gracefulShutdown.Load() {
			log.Infof("WaitSync interrupted")
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
		isSynced, _, err := p.db.IsActualBlockData(ctx)
		if err != nil {
			log.Errorf("check sync error: %v", err)
		}
		if isSynced {
			cancel()
			break
		}
		cancel()
		time.Sleep(time.Second * 3)
	}
}

func (p *WithdrawalsProcessor) makeColdWalletWithdrawals(ctx context.Context) error {
	if p.coldWallet == nil {
		return nil
	}

	tonBalance, _, err := p.bc.GetAccountCurrentState(ctx, p.wallets.TonHotWallet.Address())
	if err != nil {
		return err
	}
	dest := AddressMustFromTonutilsAddress(p.coldWallet)

	for cur, jw := range p.wallets.JettonHotWallets {
		inProgress, err := p.db.IsInProgressInternalWithdrawalRequest(ctx, dest, cur)
		if err != nil {
			return err
		}
		if inProgress {
			continue
		}
		jettonBalance, err := p.bc.GetLastJettonBalance(ctx, jw.Address)
		if err != nil {
			return err
		}
		if jettonBalance.Cmp(config.Config.Jettons[cur].HotWalletMaxCutoff) != 1 { // jettonBalance <= HotWalletMaxCutoff
			continue
		}
		jettonAmount := big.NewInt(0)
		u, err := uuid.NewV4()
		if err != nil {
			return err
		}
		jettonAmount.Sub(jettonBalance, config.Config.Jettons[cur].HotWalletResidual)
		tonBalance.Sub(tonBalance, config.JettonTransferTonAmount.Nano())
		req := WithdrawalRequest{
			Currency:    jw.Currency,
			Amount:      NewCoins(jettonAmount),
			Bounceable:  true,
			Destination: dest,
			IsInternal:  true,
			QueryID:     u.String(),
		}
		_, err = p.db.SaveWithdrawalRequest(ctx, req)
		if err != nil {
			return err
		}
		log.Infof("%v withdrawal to cold wallet saved", cur)
	}

	inProgress, err := p.db.IsInProgressInternalWithdrawalRequest(ctx, dest, TonSymbol)
	if err != nil {
		return err
	}
	if inProgress {
		return nil
	}

	if tonBalance.Cmp(config.Config.Ton.HotWalletMax) != 1 { // tonBalance <= HotWalletMax
		return nil
	}

	tonAmount := big.NewInt(0)
	u, err := uuid.NewV4()
	if err != nil {
		return err
	}
	tonAmount.Sub(tonBalance, config.Config.Ton.HotWalletResidual)
	req := WithdrawalRequest{
		Currency:    TonSymbol,
		Amount:      NewCoins(tonAmount),
		Bounceable:  p.coldWallet.IsBounceable(),
		Destination: dest,
		IsInternal:  true,
		QueryID:     u.String(),
	}

	_, err = p.db.SaveWithdrawalRequest(ctx, req)
	if err != nil {
		return err
	}
	log.Infof("TON withdrawal to cold wallet saved")
	return nil
}
