#!/bin/bash
set -e

echo "========================================="
echo "  TON Payment Processor DB Setup"
echo "========================================="
echo ""

# Цвета для вывода
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Проверка что контейнер durak-postgres запущен
if ! docker ps | grep -q durak-postgres; then
    echo -e "${RED}✗ Контейнер durak-postgres не запущен!${NC}"
    echo "Запустите его командой: docker-compose up -d postgres"
    exit 1
fi

echo -e "${GREEN}✓ Контейнер durak-postgres запущен${NC}"
echo ""

# Создание базы данных ton_payment
echo -e "${YELLOW}Создание базы данных ton_payment...${NC}"
docker exec -i durak-postgres psql -U postgres -d postgres <<-EOSQL
    SELECT 'CREATE DATABASE ton_payment'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'ton_payment')\gexec
    
    GRANT ALL PRIVILEGES ON DATABASE ton_payment TO postgres;
EOSQL

echo -e "${GREEN}✓ База данных ton_payment создана${NC}"
echo ""

# Инициализация схемы
echo -e "${YELLOW}Инициализация схемы payments...${NC}"
docker exec -i durak-postgres psql -U postgres -d ton_payment < deploy/db/01_init.up.sql

echo -e "${GREEN}✓ Схема payments создана${NC}"
echo ""

# Создание readonly пользователя для Grafana
echo -e "${YELLOW}Создание readonly пользователя для Grafana...${NC}"

# Получаем пароль из .env
if [ -f .env ]; then
    source .env
    READONLY_PASSWORD="${POSTGRES_READONLY_PASSWORD}"
else
    echo -e "${RED}✗ Файл .env не найден!${NC}"
    exit 1
fi

if [ -z "$READONLY_PASSWORD" ]; then
    echo -e "${RED}✗ POSTGRES_READONLY_PASSWORD не установлен в .env${NC}"
    exit 1
fi

docker exec -i durak-postgres psql -U postgres -d ton_payment <<-EOSQL
    -- Удаляем пользователя если существует
    DROP USER IF EXISTS pp_readonly;
    
    -- Создаем нового пользователя
    CREATE USER pp_readonly WITH PASSWORD '$READONLY_PASSWORD';
    
    -- Даем права
    GRANT CONNECT ON DATABASE ton_payment TO pp_readonly;
    GRANT USAGE ON SCHEMA payments TO pp_readonly;
    GRANT SELECT ON ALL TABLES IN SCHEMA payments TO pp_readonly;
    ALTER DEFAULT PRIVILEGES IN SCHEMA payments GRANT SELECT ON TABLES TO pp_readonly;
EOSQL

echo -e "${GREEN}✓ Readonly пользователь pp_readonly создан${NC}"
echo ""

# Проверка
echo -e "${YELLOW}Проверка созданных таблиц...${NC}"
docker exec -i durak-postgres psql -U postgres -d ton_payment <<-EOSQL
    \dt payments.*
EOSQL

echo ""
echo -e "${GREEN}=========================================${NC}"
echo -e "${GREEN}✓ База данных ton_payment готова!${NC}"
echo -e "${GREEN}=========================================${NC}"
echo ""
echo "Теперь можно запустить payment-processor:"
echo "  docker-compose up -d payment-processor"
echo ""

