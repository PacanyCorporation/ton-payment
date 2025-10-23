# ⚡ Быстрая справка

## 🔑 Важные данные

### API
```bash
Endpoint: http://localhost:8081
Token: 6ai0mw0TRJ/f51bZyRBjK1oN+YYFJ/2118+cK5AQ6sA=
```

### База данных
```bash
Host: durak-postgres
Port: 5432
Database: ton_payment
User: postgres
Password: 8nEDYexmH3qw7hDNsluv0SUiz
```

### Кошелек
```bash
Address: UQDkGoZoNc6yDeXd365NWxUXK_DflcPkS2nogFMYjfuqXNHt
Seed: then ten snack lava luggage range rabbit limb regular summer decrease cliff govern symptom arrow blood argue behave cannon inhale clog fatal stomach fancy
Status: ✅ Deployed
Network: Mainnet
```

---

## 🚀 Команды запуска

```bash
# 1. Инициализация БД
./setup-ton-payment-db.sh

# 2. Сборка образа
make -f Makefile

# 3. Запуск
docker-compose up -d payment-processor

# 4. Логи
docker logs -f payment_processor
```

---

## 📊 API примеры

### Проверка баланса
```bash
curl -H "Authorization: Bearer 6ai0mw0TRJ/f51bZyRBjK1oN+YYFJ/2118+cK5AQ6sA=" \
     http://localhost:8081/v1/balance?currency=TON
```

### Создание депозитного адреса
```bash
curl -X POST http://localhost:8081/v1/address/new \
  -H "Authorization: Bearer 6ai0mw0TRJ/f51bZyRBjK1oN+YYFJ/2118+cK5AQ6sA=" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "user123", "currency": "TON"}'
```

### Проверка депозитов
```bash
curl -H "Authorization: Bearer 6ai0mw0TRJ/f51bZyRBjK1oN+YYFJ/2118+cK5AQ6sA=" \
     "http://localhost:8081/v1/income?user_id=user123"
```

### Вывод средств
```bash
curl -X POST http://localhost:8081/v1/withdrawal/send \
  -H "Authorization: Bearer 6ai0mw0TRJ/f51bZyRBjK1oN+YYFJ/2118+cK5AQ6sA=" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user123",
    "query_id": "withdrawal001",
    "currency": "TON",
    "amount": "100000000",
    "destination": "EQC..."
  }'
```

---

## 🛠️ Управление

```bash
# Статус
docker ps | grep payment

# Перезапуск
docker-compose restart payment-processor

# Остановка
docker-compose down

# Логи
docker logs -f payment_processor
docker logs --tail 100 payment_processor

# Проверка синхронизации
curl http://localhost:8081/v1/system/sync
```

---

## 🗄️ База данных

```bash
# Подключение
docker exec -it durak-postgres psql -U postgres -d ton_payment

# Полезные запросы
\dt payments.*                    # Список таблиц
SELECT * FROM payments.deposits;  # Депозиты
SELECT * FROM payments.withdrawals; # Выводы
\q                                # Выход
```

---

## 🔧 Wallet Tool

```bash
# Генерация нового кошелька
./bin/wallet-tool generate

# Проверка кошелька
./bin/wallet-tool info \
  --seed "..." \
  --liteserver "135.181.140.212:13206" \
  --liteserver-key "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw="

# Деплой кошелька
./bin/wallet-tool deploy \
  --seed "..." \
  --liteserver "135.181.140.212:13206" \
  --liteserver-key "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw="
```

---

## 📁 Документация

| Файл | Описание |
|------|----------|
| `START_HERE.md` | 🎯 Начните отсюда |
| `INTEGRATION_GUIDE.md` | 🔗 Интеграция с БД |
| `SUMMARY.md` | 📋 Краткая сводка |
| `WALLET_SETUP_GUIDE.md` | 💼 Создание кошелька |
| `QUICK_START.md` | ⚡ Быстрый старт |
| `TESTNET_EXAMPLE.md` | 🧪 Тестирование в testnet |

---

## ⚠️ Важно помнить

- ✅ Кошелек развернут и пополнен
- ✅ API токен сгенерирован
- ✅ БД ton_payment создана
- ⚠️ Настройте COLD_WALLET в .env
- ⚠️ Регулярно делайте бэкапы БД
- ⚠️ Не храните seed фразу на сервере

---

## 🆘 Помощь

- 📖 Документация: https://gobicycle.github.io/bicycle/
- 💬 Telegram: https://t.me/tonbicycle
- 🐙 GitHub: https://github.com/gobicycle/bicycle

