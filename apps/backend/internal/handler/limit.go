package handler

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 用户维度限流：auth 之后的一道闸，防「一个真实账号拿到 token 后高频刷接口」。
//
// 为什么不是客户端签名：小程序包能被解包，签名里的密钥一定拿得到，挡不住脚本。
// 宿主机 nginx 那层按 IP 限，换代理就能绕；这里按 uid 限，同一个 token 从多少个
// IP 来都只算一份额度。
//
// 单机部署用内存令牌桶就够：重启清零无妨（几秒就补满），也不必为此引入 Redis。
// 将来扩到多实例必须换成共享存储，否则每个实例各算各的，实际放行速率是 N 倍。

type tokenBucket struct {
	tokens float64
	last   time.Time
}

type limiter struct {
	mu     sync.Mutex
	rate   float64 // 每秒补充的令牌数，即长期速率
	burst  float64 // 桶容量，即允许的瞬时并发
	states map[uint]*tokenBucket
}

func newLimiter(rate, burst float64) *limiter {
	return &limiter{rate: rate, burst: burst, states: map[uint]*tokenBucket{}}
}

// allow 取一个令牌。now 由调用方传入，测试时才能把时间往前拨而不必真等。
func (l *limiter) allow(id uint, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.states[id]
	if b == nil {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.states[id] = b
	}
	// 按经过的时间补令牌，封顶 burst：放着不用不会攒出无限额度
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep 清掉长时间没动的桶，否则 states 会随注册用户数只增不减。
func (l *limiter) sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, b := range l.states {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.states, id)
		}
	}
}

// middleware 超限返回 429。响应体与其它错误同形（{"error": ...}），
// 小程序 api.ts 会把它当普通失败弹 toast，不会误触发 401 静默重登。
func (l *limiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(uid(c), time.Now()) {
			// 令牌桶每 1/rate 秒回一个，让客户端知道别立刻重试
			c.Header("Retry-After", "1")
			fail(c, http.StatusTooManyRequests, "操作太频繁，请稍后再试")
			return
		}
		c.Next()
	}
}

// envFloat 读正的浮点配置，缺省或写错都退回 def：限流参数配坏不该让进程起不来。
func envFloat(k string, def float64) float64 {
	v, e := strconv.ParseFloat(os.Getenv(k), 64)
	if e != nil || v <= 0 {
		return def
	}
	return v
}
