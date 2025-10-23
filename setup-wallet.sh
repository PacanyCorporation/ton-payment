#!/bin/bash

# TON Payment Processor - Wallet Setup Script
# This script helps you create and deploy a HighloadV2 wallet

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default liteservers
MAINNET_LITESERVER="135.181.140.212:13206"
MAINNET_KEY="K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw="
TESTNET_LITESERVER="135.181.177.59:53312"
TESTNET_KEY="aF91CuUHuuOv9rm2W5+O/4h38M3sRm40DtSdRxQhmtQ="

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  TON HighloadV2 Wallet Setup${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if wallet-tool exists
if [ ! -f "bin/wallet-tool" ]; then
    echo -e "${YELLOW}Wallet tool not found. Building...${NC}"
    make wallet-tool
    echo -e "${GREEN}✓ Wallet tool built successfully${NC}"
    echo ""
fi

# Ask for network
echo -e "${YELLOW}Select network:${NC}"
echo "1) Mainnet (production)"
echo "2) Testnet (testing)"
read -p "Enter choice [1-2]: " network_choice

if [ "$network_choice" == "2" ]; then
    IS_TESTNET=true
    LITESERVER=$TESTNET_LITESERVER
    LITESERVER_KEY=$TESTNET_KEY
    NETWORK_NAME="TESTNET"
    echo -e "${YELLOW}Using TESTNET${NC}"
else
    IS_TESTNET=false
    LITESERVER=$MAINNET_LITESERVER
    LITESERVER_KEY=$MAINNET_KEY
    NETWORK_NAME="MAINNET"
    echo -e "${GREEN}Using MAINNET${NC}"
fi
echo ""

# Ask what to do
echo -e "${YELLOW}What would you like to do?${NC}"
echo "1) Generate new wallet"
echo "2) Check existing wallet"
echo "3) Deploy existing wallet"
read -p "Enter choice [1-3]: " action_choice
echo ""

case $action_choice in
    1)
        # Generate new wallet
        echo -e "${BLUE}Generating new HighloadV2 wallet...${NC}"
        echo ""
        
        ./bin/wallet-tool generate
        
        echo ""
        echo -e "${YELLOW}========================================${NC}"
        echo -e "${YELLOW}IMPORTANT: Save your seed phrase!${NC}"
        echo -e "${YELLOW}========================================${NC}"
        echo ""
        
        read -p "Press Enter when you have saved your seed phrase..."
        echo ""
        
        read -p "Enter your seed phrase (24 words): " SEED
        echo ""
        
        # Get wallet info
        echo -e "${BLUE}Getting wallet information...${NC}"
        echo ""
        
        if [ "$IS_TESTNET" = true ]; then
            ./bin/wallet-tool info \
                --seed "$SEED" \
                --liteserver "$LITESERVER" \
                --liteserver-key "$LITESERVER_KEY" \
                --testnet
        else
            ./bin/wallet-tool info \
                --seed "$SEED" \
                --liteserver "$LITESERVER" \
                --liteserver-key "$LITESERVER_KEY"
        fi
        
        echo ""
        echo -e "${YELLOW}========================================${NC}"
        echo -e "${YELLOW}Next steps:${NC}"
        echo -e "${YELLOW}========================================${NC}"
        echo "1. Send TON to the non-bounceable address (UQ...) shown above"
        if [ "$IS_TESTNET" = true ]; then
            echo "   Get testnet TON from: https://t.me/testgiver_ton_bot"
        else
            echo "   Minimum: 1 TON, Recommended: 10+ TON"
        fi
        echo "2. Wait 1-2 minutes for confirmation"
        echo "3. Run this script again and choose option 3 to deploy"
        echo ""
        
        # Save seed to temporary file
        echo "$SEED" > .wallet_seed.tmp
        echo -e "${GREEN}Seed saved to .wallet_seed.tmp (temporary)${NC}"
        echo -e "${RED}Remember to delete this file after deployment!${NC}"
        ;;
        
    2)
        # Check existing wallet
        if [ -f ".wallet_seed.tmp" ]; then
            echo -e "${YELLOW}Found temporary seed file. Use it? (y/n)${NC}"
            read -p "> " use_tmp
            if [ "$use_tmp" == "y" ]; then
                SEED=$(cat .wallet_seed.tmp)
            else
                read -p "Enter your seed phrase (24 words): " SEED
            fi
        else
            read -p "Enter your seed phrase (24 words): " SEED
        fi
        echo ""
        
        echo -e "${BLUE}Getting wallet information...${NC}"
        echo ""
        
        if [ "$IS_TESTNET" = true ]; then
            ./bin/wallet-tool info \
                --seed "$SEED" \
                --liteserver "$LITESERVER" \
                --liteserver-key "$LITESERVER_KEY" \
                --testnet
        else
            ./bin/wallet-tool info \
                --seed "$SEED" \
                --liteserver "$LITESERVER" \
                --liteserver-key "$LITESERVER_KEY"
        fi
        ;;
        
    3)
        # Deploy wallet
        if [ -f ".wallet_seed.tmp" ]; then
            echo -e "${YELLOW}Found temporary seed file. Use it? (y/n)${NC}"
            read -p "> " use_tmp
            if [ "$use_tmp" == "y" ]; then
                SEED=$(cat .wallet_seed.tmp)
            else
                read -p "Enter your seed phrase (24 words): " SEED
            fi
        else
            read -p "Enter your seed phrase (24 words): " SEED
        fi
        echo ""
        
        echo -e "${BLUE}Deploying HighloadV2 wallet...${NC}"
        echo ""
        
        if [ "$IS_TESTNET" = true ]; then
            ./bin/wallet-tool deploy \
                --seed "$SEED" \
                --liteserver "$LITESERVER" \
                --liteserver-key "$LITESERVER_KEY" \
                --testnet
        else
            ./bin/wallet-tool deploy \
                --seed "$SEED" \
                --liteserver "$LITESERVER" \
                --liteserver-key "$LITESERVER_KEY"
        fi
        
        echo ""
        echo -e "${GREEN}========================================${NC}"
        echo -e "${GREEN}Wallet deployed successfully!${NC}"
        echo -e "${GREEN}========================================${NC}"
        echo ""
        echo -e "${YELLOW}Next steps:${NC}"
        echo "1. Update your .env file with the seed phrase"
        echo "2. Set IS_TESTNET=$IS_TESTNET in .env"
        echo "3. Configure other parameters in .env"
        echo "4. Run: docker-compose up -d"
        echo ""
        
        # Offer to update .env
        echo -e "${YELLOW}Would you like to update .env file now? (y/n)${NC}"
        read -p "> " update_env
        
        if [ "$update_env" == "y" ]; then
            if [ -f ".env" ]; then
                # Backup existing .env
                cp .env .env.backup.$(date +%Y%m%d_%H%M%S)
                echo -e "${GREEN}Backed up existing .env${NC}"
            fi
            
            # Update SEED in .env
            if grep -q "^SEED=" .env 2>/dev/null; then
                # Use | as delimiter to avoid issues with spaces
                sed -i "s|^SEED=.*|SEED='$SEED'|" .env
            else
                echo "SEED='$SEED'" >> .env
            fi
            
            # Update IS_TESTNET in .env
            if grep -q "^IS_TESTNET=" .env 2>/dev/null; then
                sed -i "s|^IS_TESTNET=.*|IS_TESTNET=$IS_TESTNET|" .env
            else
                echo "IS_TESTNET=$IS_TESTNET" >> .env
            fi
            
            echo -e "${GREEN}✓ Updated .env file${NC}"
            echo ""
            echo -e "${YELLOW}Don't forget to configure other parameters:${NC}"
            echo "- POSTGRES_PASSWORD"
            echo "- API_TOKEN"
            echo "- COLD_WALLET"
            echo "- TON_CUTOFFS"
            echo "- LITESERVER and LITESERVER_KEY"
        fi
        
        # Clean up temporary seed file
        if [ -f ".wallet_seed.tmp" ]; then
            echo ""
            echo -e "${YELLOW}Delete temporary seed file? (y/n)${NC}"
            read -p "> " delete_tmp
            if [ "$delete_tmp" == "y" ]; then
                rm .wallet_seed.tmp
                echo -e "${GREEN}✓ Temporary seed file deleted${NC}"
            fi
        fi
        ;;
        
    *)
        echo -e "${RED}Invalid choice${NC}"
        exit 1
        ;;
esac

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Done!${NC}"
echo -e "${BLUE}========================================${NC}"

