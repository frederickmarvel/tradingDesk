document.addEventListener('DOMContentLoaded', () => {
    const asksElement = document.getElementById('asks');
    const bidsElement = document.getElementById('bids');
    const tradeAmount = document.getElementById('tradeAmount');
    const amountType = document.getElementById('amountType');
    const tradeHistoryElement = document.getElementById('tradeHistory');
    const openOrdersElement = document.getElementById('openOrders');
    const quickBuyBtn = document.getElementById('quickBuyBtn');
    const quickSellBtn = document.getElementById('quickSellBtn');
    const tradeForm = document.getElementById('tradeForm');
    const advPrice = document.getElementById('advPrice');
    const advAmount = document.getElementById('advAmount');
    const advAmountType = document.getElementById('advAmountType');
    const advTotal = document.getElementById('advTotal');
    
    let selectedQuickAmount = 100000; // Default quick trade amount
    let currentOrderbook = null;

    // Format number with commas
    function formatNumber(num) {
        return parseFloat(num).toLocaleString('en-US', {
            minimumFractionDigits: 2,
            maximumFractionDigits: 8
        });
    }

    // Handle trade execution
    async function executeTrade(type, price, isInstant) {
        const amount = parseFloat(tradeAmount.value);
        if (!amount || amount <= 0) {
            console.error('Please enter a valid amount');
            return;
        }

        const isIdr = amountType.value === 'idr';
        let tradeRequest = {
            type,
            price: parseFloat(price),
            order_type: isInstant ? 'market' : 'limit',
            time_in_force: 'GTC'
        };

        if (isIdr) {
            tradeRequest.idr_amount = amount;
        } else {
            tradeRequest.amount = amount;
        }

        try {
            const response = await fetch('/api/trade', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(tradeRequest)
            });

            const result = await response.json();
            if (result.success === 1) {
                const actualAmount = isIdr ? amount / parseFloat(price) : amount;
                addTradeToHistory(type, parseFloat(price), actualAmount, isIdr ? amount : undefined);
                fetchOpenOrders(); // Refresh open orders after successful trade
            } else {
                console.error('Trade failed:', result.error);
            }
        } catch (error) {
            console.error('Error placing order:', error.message);
        }
    }

    // Update orderbook display
    function updateOrderbook(orderbook) {
        currentOrderbook = orderbook;
        
        // Update asks (sells) - Reverse order for asks
        asksElement.innerHTML = orderbook.ask
            .slice() // Create a copy of the array
            .reverse() // Reverse the order
            .map(order => `
                <div class="order-row ask-row">
                    <span class="order-price">${formatNumber(order.price)}</span>
                    <span class="order-amount">${formatNumber(order.hnst_volume)}</span>
                    <div class="order-actions">
                        <button class="trade-btn" onclick="handleLimitBuy('${order.price}')">▲</button>
                        <button class="trade-btn" onclick="handleLimitSell('${order.price}')">▼</button>
                    </div>
                </div>
            `).join('');

        // Update bids (buys)
        bidsElement.innerHTML = orderbook.bid.map(order => `
            <div class="order-row bid-row">
                <span class="order-price">${formatNumber(order.price)}</span>
                <span class="order-amount">${formatNumber(order.hnst_volume)}</span>
                <div class="order-actions">
                    <button class="trade-btn" onclick="handleLimitBuy('${order.price}')">▲</button>
                    <button class="trade-btn" onclick="handleLimitSell('${order.price}')">▼</button>
                </div>
            </div>
        `).join('');

        // Update spread
        if (orderbook.ask.length > 0 && orderbook.bid.length > 0) {
            const lowestAsk = parseFloat(orderbook.ask[0].price);
            const highestBid = parseFloat(orderbook.bid[0].price);
            const spread = lowestAsk - highestBid;
            document.querySelector('.spread-indicator').innerHTML = `
                <span class="text-muted">Spread: ${formatNumber(spread)} IDR (${((spread/lowestAsk) * 100).toFixed(2)}%)</span>
            `;
        }
    }

    // Add global handlers for limit buy/sell
    window.handleLimitBuy = function(price) {
        executeTrade('buy', price, false);
    };

    window.handleLimitSell = function(price) {
        executeTrade('sell', price, false);
    };

    window.handleCancelOrder = function(orderId) {
        cancelOrder(orderId);
    };

    // Fetch and display open orders
    async function fetchOpenOrders() {
        try {
            const response = await fetch('/api/open-orders');
            const data = await response.json();
            if (data.success === 1) {
                updateOpenOrders(data.orders);
            } else {
                console.error('Failed to fetch open orders:', data.error);
            }
        } catch (error) {
            console.error('Error fetching open orders:', error);
        }
    }

    // Update open orders display
    function updateOpenOrders(orders) {
        openOrdersElement.innerHTML = orders.length ? orders.map(order => `
            <div class="open-order ${order.type}">
                <div class="open-order-details">
                    <strong class="text-${order.type === 'buy' ? 'success' : 'danger'}">${order.type.toUpperCase()}</strong>
                    <span class="ms-2">${formatNumber(order.amount)} HNS @ ${formatNumber(order.price)} IDR</span>
                </div>
                <button class="cancel-order-btn" onclick="handleCancelOrder('${order.id}')">×</button>
            </div>
        `).join('') : '<div class="text-muted text-center">No open orders</div>';
    }

    // Cancel order
    async function cancelOrder(orderId) {
        try {
            const response = await fetch('/api/cancel-order', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ order_id: orderId })
            });

            const result = await response.json();
            if (result.success === 1) {
                fetchOpenOrders(); // Refresh the open orders list
            } else {
                console.error('Failed to cancel order:', result.error);
            }
        } catch (error) {
            console.error('Error canceling order:', error);
        }
    }

    // Calculate and update advanced trading total
    function updateAdvancedTotal() {
        const price = parseFloat(advPrice.value) || 0;
        const amount = parseFloat(advAmount.value) || 0;
        const isIdr = advAmountType.value === 'idr';
        
        if (isIdr) {
            advTotal.value = formatNumber(amount);
        } else {
            advTotal.value = formatNumber(price * amount);
        }
    }

    // Handle advanced trading form submission
    tradeForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const type = document.querySelector('input[name="tradeType"]:checked').value;
        const price = parseFloat(advPrice.value);
        const amount = parseFloat(advAmount.value);
        const isIdr = advAmountType.value === 'idr';

        try {
            const response = await fetch('/api/trade', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    type,
                    price,
                    [isIdr ? 'idr_amount' : 'amount']: amount,
                    order_type: 'limit',
                    time_in_force: 'GTC'
                })
            });

            const result = await response.json();
            if (result.success === 1) {
                const actualAmount = isIdr ? amount / price : amount;
                addTradeToHistory(type, price, actualAmount, isIdr ? amount : undefined);
                fetchOpenOrders();
                advAmount.value = '';
                advTotal.value = '';
            } else {
                console.error('Trade failed:', result.error);
            }
        } catch (error) {
            console.error('Error placing order:', error.message);
        }
    });

    // Add event listeners for advanced trading
    advPrice.addEventListener('input', updateAdvancedTotal);
    advAmount.addEventListener('input', updateAdvancedTotal);
    advAmountType.addEventListener('change', updateAdvancedTotal);

    // Add trade to history
    function addTradeToHistory(type, price, amount, idrAmount) {
        const now = new Date();
        const timeStr = now.toLocaleTimeString();
        const tradeEntry = document.createElement('div');
        tradeEntry.className = `trade-entry ${type}`;
        
        const formattedPrice = formatNumber(price);
        const formattedAmount = formatNumber(amount);
        const formattedTotal = formatNumber(idrAmount || (price * amount));
        
        tradeEntry.innerHTML = `
            <div>
                <strong class="text-${type === 'buy' ? 'success' : 'danger'}">${type.toUpperCase()}</strong>
                <span class="ms-2">${formattedAmount} HNS @ ${formattedPrice} IDR</span>
            </div>
            <div>
                <span class="me-3">Total: ${formattedTotal} IDR</span>
                <span class="trade-time">${timeStr}</span>
            </div>
        `;
        
        tradeHistoryElement.insertBefore(tradeEntry, tradeHistoryElement.firstChild);
    }

    // Handle quick trade
    function handleQuickTrade(type) {
        if (!currentOrderbook) return;
        
        const orders = type === 'buy' ? currentOrderbook.ask : currentOrderbook.bid;
        if (orders.length === 0) return;

        const price = parseFloat(orders[0].price);
        executeTrade(type, price, true);
    }

    // Add event listeners for quick trade buttons
    quickBuyBtn.addEventListener('click', () => handleQuickTrade('buy'));
    quickSellBtn.addEventListener('click', () => handleQuickTrade('sell'));

    // Add event listeners for quick amount buttons
    document.querySelectorAll('.quick-amount').forEach(btn => {
        btn.addEventListener('click', () => {
            selectedQuickAmount = parseInt(btn.dataset.amount);
            tradeAmount.value = selectedQuickAmount;
            amountType.value = 'idr';
        });
    });

    // Fetch orderbook periodically
    async function fetchOrderbook() {
        try {
            const response = await fetch('/api/orderbook');
            const orderbook = await response.json();
            updateOrderbook(orderbook);
        } catch (error) {
            console.error('Error fetching orderbook:', error);
        }
    }

    // Initial fetch
    fetchOrderbook();
    fetchOpenOrders();

    // Update orderbook every second
    setInterval(fetchOrderbook, 1000);
    // Update open orders every 5 seconds
    setInterval(fetchOpenOrders, 5000);
}); 