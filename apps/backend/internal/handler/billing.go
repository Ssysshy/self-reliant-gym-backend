package handler

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Order struct {
	RefundID    *string `gorm:"uniqueIndex;size:96"`
	ID          string  `gorm:"primaryKey;size:32"`
	Owner       uint    `gorm:"index"`
	Product     string
	Days, Price int
	Status      string
	Platform    *string `gorm:"uniqueIndex;size:96"`
	CreatedAt   time.Time
}

func sign(key, text string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}
func purchase(c *gin.Context) {
	var in struct {
		Days int
		Code string
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "订单参数无效")
		return
	}
	var sku Product
	if e := db.Where("enabled=true AND days=?", in.Days).First(&sku).Error; e != nil {
		save(c, nil, e)
		return
	}
	price := sku.Price
	product := os.Getenv("XPAY_PRODUCT_" + strconv.Itoa(in.Days))
	offer := os.Getenv("XPAY_OFFER")
	appKey := os.Getenv("XPAY_KEY")
	if price == 0 || product == "" || offer == "" || appKey == "" || os.Getenv("WX_PUSH_TOKEN") == "" || os.Getenv("WX_PUSH_AES") == "" {
		fail(c, 503, "该套餐尚未开放购买")
		return
	}
	q := url.Values{"appid": {os.Getenv("WX_APP_ID")}, "secret": {os.Getenv("WX_SECRET")}, "grant_type": {"authorization_code"}, "js_code": {in.Code}}
	req, _ := http.NewRequestWithContext(c.Request.Context(), "GET", "https://api.weixin.qq.com/sns/jscode2session?"+q.Encode(), nil)
	client := http.Client{Timeout: 8 * time.Second}
	r, e := client.Do(req)
	if e != nil {
		fail(c, 502, "微信服务不可用")
		return
	}
	defer r.Body.Close()
	var w struct {
		OpenID string
		Key    string `json:"session_key"`
	}
	var u User
	if json.NewDecoder(r.Body).Decode(&w) != nil || w.Key == "" || db.First(&u, uid(c)).Error != nil || w.OpenID != u.OpenID {
		fail(c, 401, "请重新登录")
		return
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		fail(c, 500, "创建订单失败")
		return
	}
	order := Order{ID: hex.EncodeToString(nonce[:]), Owner: u.ID, Price: price, Days: in.Days, Product: product, Status: "待支付"}
	if e = db.Create(&order).Error; e != nil {
		save(c, nil, e)
		return
	}
	raw, _ := json.Marshal(gin.H{"offerId": offer, "buyQuantity": 1, "env": 0, "currencyType": "CNY", "productId": product, "goodsPrice": price, "outTradeNo": order.ID, "attach": order.ID})
	c.JSON(200, gin.H{"mode": "short_series_goods", "signData": string(raw), "paySig": sign(appKey, "requestVirtualPayment&"+string(raw)), "signature": sign(w.Key, string(raw))})
}
func authenticatedPush(c *gin.Context, encrypted string) bool {
	secret := os.Getenv("WX_PUSH_TOKEN")
	stamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	if secret == "" || stamp == "" || nonce == "" {
		return false
	}
	parts := []string{secret, stamp, nonce}
	signature := c.Query("signature")
	if encrypted != "" {
		parts = append(parts, encrypted)
		signature = c.Query("msg_signature")
	}
	sort.Strings(parts)
	digest := sha1.Sum([]byte(strings.Join(parts, "")))
	return hmac.Equal([]byte(signature), []byte(hex.EncodeToString(digest[:])))
}
func payload(s string) ([]byte, bool) {
	key, e := base64.RawStdEncoding.DecodeString(os.Getenv("WX_PUSH_AES"))
	data, err := base64.StdEncoding.DecodeString(s)
	if e != nil || err != nil || len(key) != 32 || len(data) < 32 || len(data)%aes.BlockSize != 0 {
		return nil, false
	}
	block, _ := aes.NewCipher(key)
	cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(data, data)
	pad := int(data[len(data)-1])
	if pad < 1 || pad > 32 || pad > len(data)-20 || !bytes.Equal(data[len(data)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return nil, false
	}
	data = data[:len(data)-pad]
	end := 20 + int(binary.BigEndian.Uint32(data[16:20]))
	if end > len(data) || string(data[end:]) != os.Getenv("WX_APP_ID") {
		return nil, false
	}
	return data[20:end], true
}
func notifications(c *gin.Context) {
	if c.Request.Method == "GET" {
		if authenticatedPush(c, "") {
			c.String(200, "%s", c.Query("echostr"))
			return
		}
		fail(c, 403, "签名无效")
		return
	}
	var wrapper struct{ Encrypt string }
	if c.ShouldBindJSON(&wrapper) != nil || wrapper.Encrypt == "" || !authenticatedPush(c, wrapper.Encrypt) {
		fail(c, 403, "签名无效")
		return
	}
	data, ok := payload(wrapper.Encrypt)
	var event struct {
		Event, OpenID, OutTradeNo string
		Env                       int
		GoodsInfo                 struct {
			ProductId             string
			Quantity, ActualPrice int
		}
		WeChatPayInfo struct{ MchOrderNo string }
	}
	if !ok || json.Unmarshal(data, &event) != nil || event.Env != 0 {
		fail(c, 422, "事件不可处理")
		return
	}
	if event.Event == "xpay_refund_notify" {
		if e := refund(data); e != nil {
			result(c, nil, e)
			return
		}
		c.String(200, "success")
		return
	}
	if event.Event != "xpay_goods_deliver_notify" {
		fail(c, 422, "事件不可处理")
		return
	}
	e := db.Transaction(func(tx *gorm.DB) error {
		var o Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&o, "id=?", event.OutTradeNo).Error; err != nil {
			return err
		}
		u, err := locked(tx, o.Owner)
		if err != nil {
			return err
		}
		platform := event.WeChatPayInfo.MchOrderNo
		if u.OpenID != event.OpenID || o.Product != event.GoodsInfo.ProductId || event.GoodsInfo.Quantity != 1 || event.GoodsInfo.ActualPrice != o.Price || platform == "" {
			return conflict
		}
		if o.Status == "已支付" {
			return nil
		}
		if o.Status != "待支付" {
			return conflict
		}
		o.Status = "已支付"
		o.Platform = &platform
		if err = tx.Save(&o).Error; err != nil {
			return err
		}
		return grantPaid(tx, o, u)
	})
	if e != nil {
		result(c, nil, e)
		return
	}
	c.String(200, "success")
}
func orders(c *gin.Context) {
	var rows []Order
	q := db.Order("created_at desc").Limit(100)
	if c.GetString("actor") == "" {
		q = q.Where("owner=?", uid(c))
	}
	e := q.Find(&rows).Error
	save(c, rows, e)
}

func refund(data []byte) error {
	var event struct {
		MchOrderId, MchRefundId, WxRefundId, OpenId string
		RetCode                                     *int
		RefundFee                                   int
	}
	if json.Unmarshal(data, &event) != nil || event.RetCode == nil || event.MchRefundId == "" || event.WxRefundId == "" {
		return conflict
	}
	if *event.RetCode != 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var o Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&o, "id=?", event.MchOrderId).Error; err != nil {
			return err
		}
		u, err := locked(tx, o.Owner)
		if err != nil {
			return err
		}
		if u.OpenID != event.OpenId || o.Price != event.RefundFee {
			return conflict
		}
		if o.Status == "已退款" {
			return nil
		}
		if o.Status == "已支付" {
			var g Grant
			if err := tx.Where("user_id=? AND operator=? AND reason=?", u.ID, "微信支付", o.ID).First(&g).Error; err != nil {
				return err
			}
			if err := revoke(tx, &g, time.Now()); err != nil {
				return err
			}
		}
		return tx.Model(&o).Updates(map[string]any{"status": "已退款", "refund_id": event.WxRefundId}).Error
	})
}
