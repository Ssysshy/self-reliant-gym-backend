package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

var channelClient = http.Client{Timeout: 8 * time.Second}
var credentials struct {
	sync.Mutex
	token string
	until time.Time
}

func wechatPost(ctx context.Context, endpoint string, body []byte, out any) error {
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := channelClient.Do(req)
	if e != nil {
		return errors.New("微信服务不可用")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("微信请求失败")
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}
func access(ctx context.Context) (string, error) {
	credentials.Lock()
	defer credentials.Unlock()
	if time.Now().Before(credentials.until) {
		return credentials.token, nil
	}
	raw, _ := json.Marshal(gin.H{"grant_type": "client_credential", "appid": os.Getenv("WX_APP_ID"), "secret": os.Getenv("WX_SECRET")})
	var res struct {
		Token  string `json:"access_token"`
		Expiry int    `json:"expires_in"`
	}
	if e := wechatPost(ctx, "https://api.weixin.qq.com/cgi-bin/stable_token", raw, &res); e != nil {
		return "", e
	}
	if res.Token == "" {
		return "", errors.New("获取微信凭证失败")
	}
	credentials.token = res.Token
	credentials.until = time.Now().Add(time.Duration(res.Expiry-120) * time.Second)
	return res.Token, nil
}
func xpay(ctx context.Context, path string, params gin.H, out any) error {
	raw, _ := json.Marshal(params)
	token, e := access(ctx)
	if e != nil {
		return e
	}
	query := url.Values{"access_token": {token}, "pay_sig": {sign(os.Getenv("XPAY_KEY"), path+"&"+string(raw))}}
	return wechatPost(ctx, "https://api.weixin.qq.com"+path+"?"+query.Encode(), raw, out)
}

type ChannelOrder struct {
	Status   int
	ID       string `json:"order_id"`
	Fee      int    `json:"paid_fee"`
	Platform string `json:"wx_order_id"`
	Type     int    `json:"order_type"`
}

func checkOrder(ctx context.Context, o Order) error {
	var u User
	if e := db.First(&u, o.Owner).Error; e != nil {
		return e
	}
	var res struct {
		Errcode int
		Order   ChannelOrder
	}
	if e := xpay(ctx, "/xpay/query_order", gin.H{"openid": u.OpenID, "env": 0, "order_id": o.ID}, &res); e != nil {
		return e
	}
	v := res.Order
	if res.Errcode != 0 || v.ID != o.ID {
		return errors.New("渠道尚未确认订单")
	}
	if v.Status == 6 {
		return db.Model(&Order{}).Where("id=? AND status=?", o.ID, "待支付").Update("status", "已关闭").Error
	}
	if v.Status != 2 {
		return nil
	}
	if v.Fee != o.Price || v.Platform == "" || (v.Type != 0 && v.Type != 7) {
		return conflict
	}
	e := db.Transaction(func(tx *gorm.DB) error {
		var current Order
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id=?", o.ID).Error; e != nil {
			return e
		}
		if current.Status == "已支付" {
			return nil
		}
		if current.Status != "待支付" {
			return conflict
		}
		u, e := locked(tx, o.Owner)
		if e != nil {
			return e
		}
		current.Status = "已支付"
		current.Platform = &v.Platform
		if e := tx.Save(&current).Error; e != nil {
			return e
		}
		return grantPaid(tx, current, u)
	})
	if e != nil {
		return e
	}
	var ack struct{ Errcode int }
	if e = xpay(ctx, "/xpay/notify_provide_goods", gin.H{"order_id": o.ID, "env": 0}, &ack); e != nil {
		return e
	}
	if ack.Errcode != 0 {
		return errors.New("等待渠道发货确认")
	}
	return nil
}
func grantPaid(tx *gorm.DB, o Order, u User) error {
	base := u.Expires
	if base.Before(time.Now()) {
		base = time.Now()
	}
	if e := tx.Create(&Grant{UserID: u.ID, Days: o.Days, Reason: o.ID, Operator: "微信支付"}).Error; e != nil {
		return e
	}
	return tx.Model(&u).Update("expires", base.AddDate(0, 0, o.Days)).Error
}
func reconcile(c *gin.Context) {
	var o Order
	q := db.Where("id=?", c.Param("id"))
	if c.GetString("actor") == "" {
		q = q.Where("owner=?", uid(c))
	}
	if e := q.First(&o).Error; e != nil {
		save(c, nil, e)
		return
	}
	if e := checkOrder(c.Request.Context(), o); e != nil {
		fail(c, 502, "渠道尚未确认，请稍后再查")
		return
	}
	save(c, true, nil)
}

func watchOrders() {
	if os.Getenv("XPAY_KEY") == "" {
		return
	}
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	var cursor string
	for range ticker.C {
		var pending []Order
		e := db.Where("status=? AND id>? AND created_at>?", "待支付", cursor, time.Now().AddDate(0, 0, -7)).Order("id").Limit(30).Find(&pending).Error
		if e != nil {
			continue
		}
		for _, o := range pending {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			_ = checkOrder(ctx, o)
			cancel()
			cursor = o.ID
		}
		if len(pending) < 30 {
			cursor = ""
		}
	}
}
