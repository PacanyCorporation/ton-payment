# 🔗 Интеграция TON Payment Processor с вашей БД

Этот гайд объясняет как TON Payment Processor интегрирован с вашей существующей PostgreSQL БД.

---

## 📊 Структура БД

### Ваша существующая PostgreSQL (durak-postgres):

```
durak-postgres (контейнер)
├── durak_dev (база данных)
├── durak_prod (база данных)
├── ton_payment (база данных) ← НОВАЯ для TON Payment Processor
└── grafana (база данных)
```

### Схема ton_payment:

```
ton_payment (база данных)
└── payments (схема)
    ├── deposits (таблица)
    ├── withdrawals (таблица)
    ├── internal_withdrawals (таблица)
    ├── jetton_masters (таблица)
    ├── jetton_wallets (таблица)
    └── service_withdrawal_requests (таблица)
```

---

## ⚙️ Что было настроено

### 1. Обновлен `.env` файл

```env
# Используем вашу существующую PostgreSQL БД
POSTGRES_DB=ton_payment
POSTGRES_USER=postgres
POSTGRES_PASSWORD=8nEDYexmH3qw7hDNsluv0SUiz
POSTGRES_READONLY_PASSWORD=/v/FZLOsSENb7SMk+6t/E+weqetxanPoo3lLS47xGRM=

# Подключение к вашему контейнеру durak-postgres
DB_URI=postgresql://postgres:8nEDYexmH3qw7hDNsluv0SUiz@durak-postgres:5432/ton_payment

# API токен (сгенерирован)
API_TOKEN=6ai0mw0TRJ/f51bZyRBjK1oN+YYFJ/2118+cK5AQ6sA=
```

### 2. Обновлен `docker-compose.yml`

- **Закомментирован** отдельный контейнер `payment-postgres`
- **payment-processor** подключается к вашему `durak-postgres`
- Добавлены сети `durak-network-dev` и `durak-network-prod`

### 3. Создан скрипт `init-db.sh`

Обновлен для создания базы `ton_payment` вместе с вашими существующими БД.

### 4. Создан скрипт `setup-ton-payment-db.sh`

Для ручной инициализации БД ton_payment в вашем контейнере.

---

## 🚀 Пошаговая инструкция запуска

### Шаг 1: Убедитесь что durak-postgres запущен

```bash
docker ps | grep durak-postgres
```

Если не запущен:
```bash
docker-compose up -d postgres  # или как у вас называется сервис
```

### Шаг 2: Инициализируйте БД ton_payment

```bash
./setup-ton-payment-db.sh
```

**Что делает скрипт:**
1. Проверяет что durak-postgres запущен
2. Создает базу данных `ton_payment`
3. Инициализирует схему `payments` с таблицами
4. Создает readonly пользователя `pp_readonly` для Grafana
5. Показывает список созданных таблиц

**Ожидаемый вывод:**
```
=========================================
  TON Payment Processor DB Setup
=========================================

✓ Контейнер durak-postgres запущен
✓ База данных ton_payment создана
✓ Схема payments создана
✓ Readonly пользователь pp_readonly создан

Проверка созданных таблиц...
                        List of relations
  Schema  |             Name              | Type  |  Owner
----------+-------------------------------+-------+----------
 payments | deposits                      | table | postgres
 payments | internal_withdrawals          | table | postgres
 payments | jetton_masters                | table | postgres
 payments | jetton_wallets                | table | postgres
 payments | service_withdrawal_requests   | table | postgres
 payments | withdrawals                   | table | postgres

=========================================
✓ База данных ton_payment готова!
=========================================
```

### Шаг 3: Соберите Docker образ payment-processor

```bash
make -f Makefile
```

### Шаг 4: Запустите payment-processor

```bash
docker-compose up -d payment-processor
```

### Шаг 5: Проверьте логи

```bash
docker logs -f payment_processor
```

**Что должно быть в логах:**
```
✓ Connected to liteserver: 135.181.140.212:13206
✓ Hot wallet loaded: EQDkGoZoNc6yDeXd365NWxUXK_DflcPkS2nogFMYjfuqXIwo
✓ Database connected: ton_payment
✓ Syncing with blockchain...
✓ API server started on :8081
```

### Шаг 6: Проверьте API

```bash
curl -H "Authorization: Bearer 6ai0mw0TRJ/f51bZyRBjK1oN+YYFJ/2118+cK5AQ6sA=" \
     http://localhost:8081/v1/balance?currency=TON
```

**Ожидаемый ответ:**
```json
{
  "balance": "10000000000",
  "currency": "TON"
}
```

---

## 🔍 Проверка БД

### Подключение к БД

```bash
docker exec -it durak-postgres psql -U postgres -d ton_payment
```

### Полезные SQL запросы

```sql
-- Список всех таблиц
\dt payments.*

-- Проверка депозитов
SELECT * FROM payments.deposits ORDER BY created_at DESC LIMIT 10;

-- Проверка выводов
SELECT * FROM payments.withdrawals ORDER BY created_at DESC LIMIT 10;

-- Статистика
SELECT 
    COUNT(*) as total_deposits,
    SUM(amount) as total_amount
FROM payments.deposits;

-- Выход
\q
```

---

## 🗂️ Структура таблиц

### deposits
Хранит информацию о входящих депозитах.

```sql
CREATE TABLE payments.deposits (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    lt BIGINT NOT NULL,
    amount NUMERIC NOT NULL,
    currency TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
```

### withdrawals
Хранит информацию об исходящих выводах.

```sql
CREATE TABLE payments.withdrawals (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    query_id TEXT NOT NULL UNIQUE,
    amount NUMERIC NOT NULL,
    currency TEXT NOT NULL,
    destination TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
```

---

## 🔐 Пользователи БД

### postgres (основной)
- **Логин:** `postgres`
- **Пароль:** `8nEDYexmH3qw7hDNsluv0SUiz`
- **Права:** Полный доступ ко всем БД
- **Используется:** payment-processor

### pp_readonly (только чтение)
- **Логин:** `pp_readonly`
- **Пароль:** `/v/FZLOsSENb7SMk+6t/E+weqetxanPoo3lLS47xGRM=`
- **Права:** SELECT на схему payments
- **Используется:** Grafana для мониторинга

---

## 📊 Grafana (опционально)

Если хотите использовать Grafana для мониторинга:

```bash
docker-compose up -d payment-grafana
```

**Доступ:**
- URL: http://localhost:3001
- Логин: `admin`
- Пароль: `admin` (измените после первого входа!)

**Datasource уже настроен:**
- Host: `durak-postgres:5432`
- Database: `ton_payment`
- User: `pp_readonly`
- Password: из .env

---

## 🛠️ Troubleshooting

### Проблема: "connection refused" к durak-postgres

**Решение:**
```bash
# Проверьте что контейнер запущен
docker ps | grep durak-postgres

# Проверьте сети
docker network ls | grep durak

# Проверьте что payment-processor в правильной сети
docker inspect payment_processor | grep -A 10 Networks
```

### Проблема: "database ton_payment does not exist"

**Решение:**
```bash
# Запустите скрипт инициализации
./setup-ton-payment-db.sh
```

### Проблема: "permission denied for schema payments"

**Решение:**
```bash
# Пересоздайте readonly пользователя
docker exec -i durak-postgres psql -U postgres -d ton_payment <<-EOSQL
    DROP USER IF EXISTS pp_readonly;
    CREATE USER pp_readonly WITH PASSWORD '/v/FZLOsSENb7SMk+6t/E+weqetxanPoo3lLS47xGRM=';
    GRANT CONNECT ON DATABASE ton_payment TO pp_readonly;
    GRANT USAGE ON SCHEMA payments TO pp_readonly;
    GRANT SELECT ON ALL TABLES IN SCHEMA payments TO pp_readonly;
    ALTER DEFAULT PRIVILEGES IN SCHEMA payments GRANT SELECT ON TABLES TO pp_readonly;
EOSQL
```

---

## 📁 Файлы конфигурации

```
ton-payment/
├── .env                        ← Основная конфигурация
├── docker-compose.yml          ← Обновлен для использования durak-postgres
├── init-db.sh                  ← Скрипт для docker-entrypoint-initdb.d
├── setup-ton-payment-db.sh     ← Ручная инициализация БД
└── deploy/db/
    ├── 01_init.up.sql          ← SQL схема
    └── 02_create_readonly_user.sh
```

---

## ✅ Чек-лист

- [ ] durak-postgres запущен
- [ ] База ton_payment создана (`./setup-ton-payment-db.sh`)
- [ ] Образ payment-processor собран (`make -f Makefile`)
- [ ] payment-processor запущен (`docker-compose up -d payment-processor`)
- [ ] Логи показывают успешное подключение
- [ ] API отвечает на запросы
- [ ] Тестовый депозит создан и зарегистрирован

---

## 🎯 Следующие шаги

1. ✅ Протестируйте создание депозитных адресов
2. ✅ Протестируйте получение платежей
3. ✅ Протестируйте вывод средств
4. ✅ Настройте мониторинг в Grafana (опционально)
5. ✅ Настройте бэкапы БД
6. ✅ Интегрируйте с вашим приложением

---

**Готово! TON Payment Processor интегрирован с вашей БД!** 🎉

