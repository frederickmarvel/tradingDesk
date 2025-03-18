package main

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	indodaxWSURL  = "wss://ws3.indodax.com/ws/"
	indodaxAPIURL = "https://indodax.com/tapi"
	staticToken   = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE5NDY2MTg0MTV9.UR1lBM6Eqh0yWz-PVirw1uPCxe60FdchR8eNVdsskeo"
	// Add these constants for authentication
	adminUsername = "admin"
	adminPassword = "admin" // Change this to a secure password
)

// Add session management
var (
	sessions = make(map[string]bool)
	sessMu   sync.RWMutex
)

// Add authentication middleware
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for session cookie
		sessionID, err := c.Cookie("session_id")
		if err != nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		// Validate session
		sessMu.RLock()
		valid := sessions[sessionID]
		sessMu.RUnlock()

		if !valid {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		c.Next()
	}
}

// Add middleware to redirect authenticated users away from login page
func redirectIfAuthenticated() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, err := c.Cookie("session_id")
		if err != nil {
			c.Next()
			return
		}

		sessMu.RLock()
		valid := sessions[sessionID]
		sessMu.RUnlock()

		if valid {
			c.Redirect(http.StatusFound, "/")
			c.Abort()
			return
		}

		c.Next()
	}
}

// Configuration struct for API credentials
type Config struct {
	APIKey    string
	APISecret string
}

type OrderBook struct {
	Pair string  `json:"pair"`
	Ask  []Order `json:"ask"`
	Bid  []Order `json:"bid"`
}

type Order struct {
	Price     string `json:"price"`
	HNSVolume string `json:"hnst_volume"`
	IDRVolume string `json:"idr_volume"`
}

type TradeRequest struct {
	Type        string  `json:"type"`
	Price       float64 `json:"price"`
	Amount      float64 `json:"amount"`
	IDRAmount   float64 `json:"idr_amount"`
	OrderType   string  `json:"order_type"`
	TimeInForce string  `json:"time_in_force"`
}

type TradeResponse struct {
	Success int                    `json:"success"`
	Return  map[string]interface{} `json:"return,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

type OpenOrder struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Price  float64 `json:"price"`
	Amount float64 `json:"amount"`
}

type WSClient struct {
	conn      *websocket.Conn
	orderBook OrderBook
	mu        sync.RWMutex
	config    Config
}

func (c *WSClient) connectWS() error {
	conn, _, err := websocket.DefaultDialer.Dial(indodaxWSURL, nil)
	if err != nil {
		return err
	}
	c.conn = conn

	// Authenticate
	authReq := map[string]interface{}{
		"params": map[string]string{
			"token": staticToken,
		},
		"id": 1,
	}

	if err := conn.WriteJSON(authReq); err != nil {
		return err
	}

	// Subscribe to orderbook
	subReq := map[string]interface{}{
		"method": 1,
		"params": map[string]string{
			"channel": "market:order-book-hnstidr",
		},
		"id": 2,
	}

	return conn.WriteJSON(subReq)
}

func (c *WSClient) handleMessages() {
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("read error: %v", err)
			c.reconnect()
			continue
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(message, &resp); err != nil {
			log.Printf("json unmarshal error: %v", err)
			continue
		}

		if result, ok := resp["result"].(map[string]interface{}); ok {
			if data, ok := result["data"].(map[string]interface{}); ok {
				if data, ok := data["data"].(map[string]interface{}); ok {
					c.mu.Lock()
					c.orderBook.Pair = data["pair"].(string)

					// Parse ask orders
					if askData, ok := data["ask"].([]interface{}); ok {
						c.orderBook.Ask = make([]Order, len(askData))
						for i, ask := range askData {
							askMap := ask.(map[string]interface{})
							c.orderBook.Ask[i] = Order{
								Price:     askMap["price"].(string),
								HNSVolume: askMap["hnst_volume"].(string),
								IDRVolume: askMap["idr_volume"].(string),
							}
						}
					}

					// Parse bid orders
					if bidData, ok := data["bid"].([]interface{}); ok {
						c.orderBook.Bid = make([]Order, len(bidData))
						for i, bid := range bidData {
							bidMap := bid.(map[string]interface{})
							c.orderBook.Bid[i] = Order{
								Price:     bidMap["price"].(string),
								HNSVolume: bidMap["hnst_volume"].(string),
								IDRVolume: bidMap["idr_volume"].(string),
							}
						}
					}
					c.mu.Unlock()
				}
			}
		}
	}
}

func (c *WSClient) reconnect() {
	for {
		log.Println("Attempting to reconnect...")
		if err := c.connectWS(); err == nil {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func (c *WSClient) GetOrderBook() OrderBook {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.orderBook
}

func (c *WSClient) placeTrade(trade TradeRequest) (*TradeResponse, error) {
	timestamp := time.Now().UnixMilli()

	// Prepare parameters
	params := url.Values{}
	params.Set("method", "trade")
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	params.Set("pair", "hnst_idr")
	params.Set("type", trade.Type)

	// Set order type and time in force
	if trade.OrderType != "" {
		params.Set("order_type", trade.OrderType)
	} else {
		params.Set("order_type", "limit") // default to limit order
	}

	if trade.TimeInForce != "" {
		params.Set("time_in_force", trade.TimeInForce)
	} else {
		params.Set("time_in_force", "GTC") // default to GTC
	}

	// Format price with 8 decimal places
	params.Set("price", strconv.FormatFloat(trade.Price, 'f', 8, 64))

	// Set amount based on trade type and amount type
	if trade.IDRAmount > 0 && trade.Type == "buy" {
		params.Set("idr", strconv.FormatFloat(trade.IDRAmount, 'f', 8, 64))
	} else {
		params.Set("hnst", strconv.FormatFloat(trade.Amount, 'f', 8, 64))
	}

	// Calculate signature
	signature := c.generateSignature(params.Encode())

	// Create request
	req, err := http.NewRequest("POST", indodaxAPIURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	// Add headers
	req.Header.Set("Key", c.config.APIKey)
	req.Header.Set("Sign", signature)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	// Parse response
	var tradeResp TradeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tradeResp); err != nil {
		return nil, fmt.Errorf("error decoding response: %v", err)
	}

	// Convert numeric success to error message if needed
	if tradeResp.Success == 0 && tradeResp.Error == "" {
		tradeResp.Error = "Trade request failed"
	}

	return &tradeResp, nil
}

func (c *WSClient) generateSignature(payload string) string {
	mac := hmac.New(sha512.New, []byte(c.config.APISecret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (c *WSClient) getOpenOrders() ([]OpenOrder, error) {
	timestamp := time.Now().UnixMilli()

	// Prepare parameters
	params := url.Values{}
	params.Set("method", "openOrders")
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	params.Set("pair", "hnst_idr")

	// Calculate signature
	signature := c.generateSignature(params.Encode())

	// Create request
	req, err := http.NewRequest("POST", indodaxAPIURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	// Add headers
	req.Header.Set("Key", c.config.APIKey)
	req.Header.Set("Sign", signature)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	// Parse response
	var result struct {
		Success int                      `json:"success"`
		Return  map[string][]interface{} `json:"return"`
		Error   string                   `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("error decoding response: %v", err)
	}

	if result.Success == 0 {
		return nil, fmt.Errorf(result.Error)
	}

	// Parse orders
	var orders []OpenOrder
	if orderList, ok := result.Return["orders"]; ok {
		for _, o := range orderList {
			order, ok := o.(map[string]interface{})
			if !ok {
				continue
			}

			// Safely get order ID
			orderID, ok := order["order_id"].(string)
			if !ok {
				continue
			}

			// Safely get order type
			orderType, ok := order["type"].(string)
			if !ok {
				continue
			}

			// Safely get price
			priceStr, ok := order["price"].(string)
			if !ok {
				continue
			}
			price := parseFloat(priceStr)

			// Safely get remaining amount
			amountStr, ok := order["remaining_amount"].(string)
			if !ok {
				continue
			}
			amount := parseFloat(amountStr)

			orders = append(orders, OpenOrder{
				ID:     orderID,
				Type:   orderType,
				Price:  price,
				Amount: amount,
			})
		}
	}

	return orders, nil
}

func (c *WSClient) cancelOrder(orderID string) error {
	timestamp := time.Now().UnixMilli()

	// Prepare parameters
	params := url.Values{}
	params.Set("method", "cancelOrder")
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	params.Set("pair", "hnst_idr")
	params.Set("order_id", orderID)

	// Calculate signature
	signature := c.generateSignature(params.Encode())

	// Create request
	req, err := http.NewRequest("POST", indodaxAPIURL, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}

	// Add headers
	req.Header.Set("Key", c.config.APIKey)
	req.Header.Set("Sign", signature)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	// Parse response
	var result struct {
		Success int    `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("error decoding response: %v", err)
	}

	if result.Success == 0 {
		return fmt.Errorf(result.Error)
	}

	return nil
}

func main() {
	wsClient := &WSClient{
		config: Config{
			APIKey:    "yourApiKey",
			APISecret: "yourApiSecret",
		},
	}

	if err := wsClient.connectWS(); err != nil {
		log.Fatal("Failed to connect to WebSocket:", err)
	}
	defer wsClient.conn.Close()

	go wsClient.handleMessages()

	r := gin.Default()

	// Serve static files
	r.Static("/static", "./static")
	r.LoadHTMLGlob("templates/*")

	// Login routes (unprotected)
	loginGroup := r.Group("/")
	loginGroup.Use(redirectIfAuthenticated())
	{
		loginGroup.GET("/login", func(c *gin.Context) {
			c.HTML(http.StatusOK, "login.html", nil)
		})

		loginGroup.POST("/login", func(c *gin.Context) {
			username := c.PostForm("username")
			password := c.PostForm("password")

			if username != adminUsername || password != adminPassword {
				c.HTML(http.StatusUnauthorized, "login.html", gin.H{"error": "Invalid credentials"})
				return
			}

			// Create session
			sessionID := fmt.Sprintf("%d", time.Now().UnixNano())
			sessMu.Lock()
			sessions[sessionID] = true
			sessMu.Unlock()

			// Set session cookie
			c.SetCookie("session_id", sessionID, 3600, "/", "", false, true)
			c.Redirect(http.StatusFound, "/")
		})
	}

	// Logout route (protected)
	r.GET("/logout", authMiddleware(), func(c *gin.Context) {
		sessionID, _ := c.Cookie("session_id")
		sessMu.Lock()
		delete(sessions, sessionID)
		sessMu.Unlock()
		c.SetCookie("session_id", "", -1, "/", "", false, true)
		c.Redirect(http.StatusFound, "/login")
	})

	// Protected routes
	protected := r.Group("/")
	protected.Use(authMiddleware())
	{
		protected.GET("/", func(c *gin.Context) {
			c.HTML(http.StatusOK, "index.html", nil)
		})

		protected.GET("/api/orderbook", func(c *gin.Context) {
			c.JSON(http.StatusOK, wsClient.GetOrderBook())
		})

		protected.GET("/api/open-orders", func(c *gin.Context) {
			orders, err := wsClient.getOpenOrders()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": 0, "error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": 1, "orders": orders})
		})

		protected.POST("/api/trade", func(c *gin.Context) {
			var trade TradeRequest
			if err := c.BindJSON(&trade); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": 0, "error": "Invalid request format"})
				return
			}

			if trade.OrderType == "" {
				trade.OrderType = "limit"
			}
			if trade.TimeInForce == "" {
				trade.TimeInForce = "GTC"
			}

			result, err := wsClient.placeTrade(trade)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": 0, "error": err.Error()})
				return
			}

			if result.Success == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"success": 0, "error": result.Error})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"success": 1,
				"return":  result.Return,
			})
		})

		protected.POST("/api/cancel-order", func(c *gin.Context) {
			var req struct {
				OrderID string `json:"order_id" binding:"required"`
			}
			if err := c.BindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": 0, "error": "Invalid request format"})
				return
			}

			if err := wsClient.cancelOrder(req.OrderID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": 0, "error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{"success": 1})
		})
	}

	log.Fatal(r.Run(":80"))
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
