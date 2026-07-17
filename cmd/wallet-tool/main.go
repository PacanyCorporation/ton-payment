package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

const (
	Version = "1.0.0"
)

func main() {
	fmt.Printf("TON HighloadV2 Wallet Tool v%s\n\n", Version)

	// Флаги командной строки
	generateCmd := flag.NewFlagSet("generate", flag.ExitOnError)

	deployCmd := flag.NewFlagSet("deploy", flag.ExitOnError)
	deploySeed := deployCmd.String("seed", "", "Seed phrase (24 words)")
	deployLiteserver := deployCmd.String("liteserver", "", "Liteserver address (IP:PORT)")
	deployLiteserverKey := deployCmd.String("liteserver-key", "", "Liteserver public key")
	deployTestnet := deployCmd.Bool("testnet", false, "Use testnet")

	infoCmd := flag.NewFlagSet("info", flag.ExitOnError)
	infoSeed := infoCmd.String("seed", "", "Seed phrase (24 words)")
	infoLiteserver := infoCmd.String("liteserver", "", "Liteserver address (IP:PORT)")
	infoLiteserverKey := infoCmd.String("liteserver-key", "", "Liteserver public key")
	infoTestnet := infoCmd.Bool("testnet", false, "Use testnet")

	sweepCmd := flag.NewFlagSet("sweep", flag.ExitOnError)
	sweepSeed := sweepCmd.String("seed", "", "Seed phrase (24 words)")
	sweepLiteserver := sweepCmd.String("liteserver", "", "Liteserver address (IP:PORT)")
	sweepLiteserverKey := sweepCmd.String("liteserver-key", "", "Liteserver public key")
	sweepTestnet := sweepCmd.Bool("testnet", false, "Use testnet")
	sweepTo := sweepCmd.String("to", "", "Destination address")
	sweepYes := sweepCmd.Bool("yes", false, "Skip confirmation prompt")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "generate":
		generateCmd.Parse(os.Args[2:])
		generateWallet()
	case "deploy":
		deployCmd.Parse(os.Args[2:])
		if *deploySeed == "" || *deployLiteserver == "" || *deployLiteserverKey == "" {
			fmt.Println("Error: --seed, --liteserver and --liteserver-key are required")
			deployCmd.PrintDefaults()
			os.Exit(1)
		}
		deployWallet(*deploySeed, *deployLiteserver, *deployLiteserverKey, *deployTestnet)
	case "info":
		infoCmd.Parse(os.Args[2:])
		if *infoSeed == "" || *infoLiteserver == "" || *infoLiteserverKey == "" {
			fmt.Println("Error: --seed, --liteserver and --liteserver-key are required")
			infoCmd.PrintDefaults()
			os.Exit(1)
		}
		getWalletInfo(*infoSeed, *infoLiteserver, *infoLiteserverKey, *infoTestnet)
	case "sweep":
		sweepCmd.Parse(os.Args[2:])
		if *sweepSeed == "" || *sweepLiteserver == "" || *sweepLiteserverKey == "" || *sweepTo == "" {
			fmt.Println("Error: --seed, --liteserver, --liteserver-key and --to are required")
			sweepCmd.PrintDefaults()
			os.Exit(1)
		}
		sweepWallet(*sweepSeed, *sweepLiteserver, *sweepLiteserverKey, *sweepTo, *sweepTestnet, *sweepYes)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  wallet-tool generate")
	fmt.Println("      Generate new seed phrase for HighloadV2 wallet")
	fmt.Println()
	fmt.Println("  wallet-tool info --seed \"SEED\" --liteserver \"IP:PORT\" --liteserver-key \"KEY\" [--testnet]")
	fmt.Println("      Get wallet address and balance")
	fmt.Println()
	fmt.Println("  wallet-tool deploy --seed \"SEED\" --liteserver \"IP:PORT\" --liteserver-key \"KEY\" [--testnet]")
	fmt.Println("      Deploy HighloadV2 wallet (requires balance on wallet)")
	fmt.Println()
	fmt.Println("  wallet-tool sweep --seed \"SEED\" --liteserver \"IP:PORT\" --liteserver-key \"KEY\" --to \"ADDRESS\" [--testnet] [--yes]")
	fmt.Println("      Send ALL TON from the wallet to the destination address")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Generate new seed")
	fmt.Println("  wallet-tool generate")
	fmt.Println()
	fmt.Println("  # Get wallet info")
	fmt.Println("  wallet-tool info --seed \"word1 word2 ... word24\" \\")
	fmt.Println("    --liteserver \"135.181.140.212:13206\" \\")
	fmt.Println("    --liteserver-key \"K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=\"")
	fmt.Println()
	fmt.Println("  # Deploy wallet")
	fmt.Println("  wallet-tool deploy --seed \"word1 word2 ... word24\" \\")
	fmt.Println("    --liteserver \"135.181.140.212:13206\" \\")
	fmt.Println("    --liteserver-key \"K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=\"")
}

func generateWallet() {
	seed := wallet.NewSeed()
	fmt.Println("=== New HighloadV2 Wallet Seed Generated ===")
	fmt.Println()
	fmt.Println("IMPORTANT: Save this seed phrase in a secure place!")
	fmt.Println("You will need it to access your wallet.")
	fmt.Println()
	fmt.Printf("Seed phrase (24 words):\n%s\n", strings.Join(seed, " "))
	fmt.Println()
	fmt.Println("=== Next Steps ===")
	fmt.Println("1. Save the seed phrase securely")
	fmt.Println("2. Get wallet address using: wallet-tool info --seed \"...\" --liteserver \"...\" --liteserver-key \"...\"")
	fmt.Println("3. Send TON to the wallet address (minimum 1 TON recommended)")
	fmt.Println("4. Deploy wallet using: wallet-tool deploy --seed \"...\" --liteserver \"...\" --liteserver-key \"...\"")
}

func connectToBlockchain(liteserverAddr, liteserverKey string) (*ton.APIClient, error) {
	client := liteclient.NewConnectionPool()

	err := client.AddConnection(context.Background(), liteserverAddr, liteserverKey)
	if err != nil {
		return nil, fmt.Errorf("connection error: %w", err)
	}

	api := ton.NewAPIClient(client)
	return api, nil
}

func getWalletInfo(seedPhrase, liteserverAddr, liteserverKey string, isTestnet bool) {
	fmt.Println("=== Connecting to TON blockchain ===")

	api, err := connectToBlockchain(liteserverAddr, liteserverKey)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	words := strings.Split(seedPhrase, " ")
	if len(words) != 24 {
		log.Fatalf("Invalid seed phrase: expected 24 words, got %d", len(words))
	}

	fmt.Println("Generating HighloadV2 wallet from seed...")

	w, err := wallet.FromSeed(api, words, wallet.HighloadV2R2)
	if err != nil {
		log.Fatalf("Failed to generate wallet: %v", err)
	}

	addr := w.Address()
	addr.SetTestnetOnly(isTestnet)

	fmt.Println()
	fmt.Println("=== Wallet Information ===")
	fmt.Printf("Type: HighloadV2R2\n")
	fmt.Printf("Address (bounceable): %s\n", addr.String())

	addr.SetBounce(false)
	fmt.Printf("Address (non-bounceable): %s\n", addr.String())
	addr.SetBounce(true)

	fmt.Printf("Testnet: %v\n", isTestnet)
	fmt.Printf("Shard: %d (0x%02x)\n", addr.Data()[0], addr.Data()[0])
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Fetching balance and status...")

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		log.Fatalf("Failed to get masterchain info: %v", err)
	}

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		log.Fatalf("Failed to get balance: %v", err)
	}

	state, err := api.GetAccount(ctx, block, addr)
	if err != nil {
		log.Fatalf("Failed to get account state: %v", err)
	}

	balanceNano := balance.Nano()
	fmt.Printf("Balance: %s TON (%s nanoTON)\n",
		formatTON(balanceNano),
		balanceNano.String())

	// Check if account state exists
	var accountStatus string
	if state.State != nil {
		accountStatus = string(state.State.Status)
	} else {
		accountStatus = "Uninitialized"
	}
	fmt.Printf("Status: %s\n", accountStatus)
	fmt.Println()

	if state.State != nil && state.State.Status == tlb.AccountStatusActive {
		fmt.Println("✓ Wallet is ACTIVE and ready to use!")
	} else if balanceNano.Cmp(big.NewInt(0)) > 0 {
		fmt.Println("⚠ Wallet has balance but is NOT DEPLOYED yet")
		fmt.Println("  Run 'wallet-tool deploy' to deploy the wallet")
	} else {
		fmt.Println("⚠ Wallet is NOT DEPLOYED and has ZERO balance")
		fmt.Println("  Send TON to the address above, then run 'wallet-tool deploy'")
	}
	fmt.Println()

	// Рекомендации для .env
	fmt.Println("=== Configuration for .env file ===")
	fmt.Printf("SEED='%s'\n", seedPhrase)
	fmt.Printf("IS_TESTNET=%v\n", isTestnet)

	minBalance := big.NewInt(1_000_000_000) // 1 TON
	if balanceNano.Cmp(minBalance) >= 0 {
		fmt.Println()
		fmt.Println("✓ Balance is sufficient for payment processor (>= 1 TON)")
	} else {
		fmt.Println()
		fmt.Printf("⚠ Recommended minimum balance: 1 TON (current: %s TON)\n", formatTON(balanceNano))
	}
}

func deployWallet(seedPhrase, liteserverAddr, liteserverKey string, isTestnet bool) {
	fmt.Println("=== Deploying HighloadV2 Wallet ===")

	api, err := connectToBlockchain(liteserverAddr, liteserverKey)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	words := strings.Split(seedPhrase, " ")
	if len(words) != 24 {
		log.Fatalf("Invalid seed phrase: expected 24 words, got %d", len(words))
	}

	fmt.Println("Generating wallet from seed...")

	w, err := wallet.FromSeed(api, words, wallet.HighloadV2R2)
	if err != nil {
		log.Fatalf("Failed to generate wallet: %v", err)
	}

	addr := w.Address()
	addr.SetTestnetOnly(isTestnet)

	fmt.Printf("Wallet address: %s\n", addr.String())
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	fmt.Println("Checking current status...")

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		log.Fatalf("Failed to get masterchain info: %v", err)
	}

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		log.Fatalf("Failed to get balance: %v", err)
	}

	state, err := api.GetAccount(ctx, block, addr)
	if err != nil {
		log.Fatalf("Failed to get account state: %v", err)
	}

	balanceNano := balance.Nano()
	fmt.Printf("Current balance: %s TON\n", formatTON(balanceNano))

	var accountStatus string
	if state.State != nil {
		accountStatus = string(state.State.Status)
	} else {
		accountStatus = "Uninitialized"
	}
	fmt.Printf("Current status: %s\n", accountStatus)
	fmt.Println()

	if balanceNano.Cmp(big.NewInt(0)) == 0 {
		log.Fatalf("Error: Wallet has zero balance. Send TON to %s first", addr.String())
	}

	if state.State != nil && state.State.Status == tlb.AccountStatusActive {
		fmt.Println("✓ Wallet is already ACTIVE!")
		fmt.Println("No deployment needed.")
		return
	}

	fmt.Println("Deploying wallet contract...")
	fmt.Println("Sending deployment transaction...")

	// Отправляем транзакцию самому себе с нулевой суммой для деплоя
	err = w.TransferNoBounce(ctx, addr, tlb.FromNanoTONU(0), "deploy")
	if err != nil {
		log.Fatalf("Failed to send deployment transaction: %v", err)
	}

	fmt.Println("Transaction sent! Waiting for confirmation...")

	// Ждем активации
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)

		block, err = api.CurrentMasterchainInfo(ctx)
		if err != nil {
			continue
		}

		state, err = api.GetAccount(ctx, block, addr)
		if err != nil {
			continue
		}

		if state.State != nil && state.State.Status == tlb.AccountStatusActive {
			fmt.Println()
			fmt.Println("✓ SUCCESS! Wallet deployed and ACTIVE!")
			fmt.Printf("Address: %s\n", addr.String())

			balance, _ := w.GetBalance(ctx, block)
			fmt.Printf("Balance: %s TON\n", formatTON(balance.Nano()))
			fmt.Println()
			fmt.Println("You can now use this wallet with the payment processor!")
			return
		}

		fmt.Print(".")
	}

	fmt.Println()
	log.Fatalf("Timeout waiting for wallet activation. Please check manually.")
}

func sweepWallet(seedPhrase, liteserverAddr, liteserverKey, toAddr string, isTestnet, skipConfirm bool) {
	fmt.Println("=== Sweep ALL TON from wallet ===")

	dst, err := address.ParseAddr(toAddr)
	if err != nil {
		log.Fatalf("Invalid destination address: %v", err)
	}

	api, err := connectToBlockchain(liteserverAddr, liteserverKey)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	words := strings.Split(seedPhrase, " ")
	if len(words) != 24 {
		log.Fatalf("Invalid seed phrase: expected 24 words, got %d", len(words))
	}

	w, err := wallet.FromSeed(api, words, wallet.HighloadV2R2)
	if err != nil {
		log.Fatalf("Failed to generate wallet: %v", err)
	}

	addr := w.Address()
	addr.SetTestnetOnly(isTestnet)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		log.Fatalf("Failed to get masterchain info: %v", err)
	}

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		log.Fatalf("Failed to get balance: %v", err)
	}
	balanceNano := balance.Nano()

	state, err := api.GetAccount(ctx, block, addr)
	if err != nil {
		log.Fatalf("Failed to get account state: %v", err)
	}
	if state.State == nil || state.State.Status != tlb.AccountStatusActive {
		log.Fatalf("Wallet is not active — nothing to sweep or deploy it first")
	}
	if balanceNano.Cmp(big.NewInt(0)) <= 0 {
		log.Fatalf("Wallet balance is zero — nothing to sweep")
	}

	fmt.Printf("From:    %s\n", addr.String())
	fmt.Printf("To:      %s\n", dst.String())
	fmt.Printf("Balance: %s TON — will be sent ENTIRELY (minus network fee)\n", formatTON(balanceNano))
	fmt.Println()
	fmt.Println("WARNING: this drains the payment processor hot wallet.")
	fmt.Println("Withdrawals and jetton operations will fail until it is refilled.")
	fmt.Println()

	if !skipConfirm {
		fmt.Print("Type 'yes' to confirm: ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	body, err := wallet.CreateCommentCell("sweep")
	if err != nil {
		log.Fatalf("Failed to build comment cell: %v", err)
	}

	// Mode 128 = carry all remaining balance; сумма в сообщении игнорируется.
	// Bounce=false — дойдёт даже на неинициализированный адрес.
	msg := &wallet.Message{
		Mode: 128,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      false,
			DstAddr:     dst,
			Amount:      tlb.FromNanoTONU(0),
			Body:        body,
		},
	}

	fmt.Println("Sending sweep transaction...")
	if err := w.Send(ctx, msg, true); err != nil {
		log.Fatalf("Failed to send: %v", err)
	}

	fmt.Println("Transaction confirmed. Checking final balance...")
	block, err = api.CurrentMasterchainInfo(ctx)
	if err == nil {
		if b, err := w.GetBalance(ctx, block); err == nil {
			fmt.Printf("Remaining balance: %s TON\n", formatTON(b.Nano()))
		}
	}
	fmt.Println("✓ Sweep complete")
}

func formatTON(nanoTON *big.Int) string {
	if nanoTON == nil {
		return "0"
	}

	ton := new(big.Float).SetInt(nanoTON)
	ton = ton.Quo(ton, big.NewFloat(1_000_000_000))

	return ton.Text('f', 9)
}
