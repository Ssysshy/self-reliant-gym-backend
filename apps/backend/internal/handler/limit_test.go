package handler

import (
	"testing"
	"time"
)

// 桶满 → 放行 burst 次 → 第 burst+1 次拦下。这是最容易被改坏的边界。
func TestLimiterBurstThenRefill(t *testing.T) {
	l := newLimiter(2, 3) // 每秒补 2 个，容量 3
	now := time.Unix(1000, 0)

	for i := 1; i <= 3; i++ {
		if !l.allow(1, now) {
			t.Fatalf("第 %d 次就被拦了，容量是 3", i)
		}
	}
	if l.allow(1, now) {
		t.Fatal("桶空了还放行")
	}

	// 0.5 秒补 1 个：只够一次
	half := now.Add(500 * time.Millisecond)
	if !l.allow(1, half) {
		t.Fatal("补了 1 个令牌却没放行")
	}
	if l.allow(1, half) {
		t.Fatal("只补了 1 个，不该放行第二个")
	}

	// 久放只补到容量，不攒上限
	hour := now.Add(time.Hour)
	for i := 1; i <= 3; i++ {
		if !l.allow(1, hour) {
			t.Fatalf("放了一小时应当补满 3 个，第 %d 次被拦", i)
		}
	}
	if l.allow(1, hour) {
		t.Fatal("补满后第 4 次不该放行，说明令牌攒过了容量")
	}
}

func TestLimiterIsolatesUsers(t *testing.T) {
	l := newLimiter(1, 1)
	now := time.Unix(1000, 0)
	if !l.allow(1, now) || !l.allow(2, now) {
		t.Fatal("不同用户应当各有额度")
	}
	if l.allow(1, now) || l.allow(2, now) {
		t.Fatal("额度不该跨用户共享")
	}
}

func TestSweepDropsIdleOnly(t *testing.T) {
	l := newLimiter(1, 1)
	now := time.Unix(1000, 0)
	l.allow(1, now)
	l.allow(2, now.Add(9*time.Minute))

	l.sweep(now.Add(11 * time.Minute))
	if _, ok := l.states[1]; ok {
		t.Fatal("空闲超过 10 分钟的桶应当被清掉")
	}
	if _, ok := l.states[2]; !ok {
		t.Fatal("刚用过的桶不该被清")
	}
}

func TestEnvFloatFallsBack(t *testing.T) {
	// 空、非数字、0、负数都退回默认值：限流参数配错不该变成「全拦」或「不拦」
	for _, v := range []string{"", "abc", "0", "-1"} {
		t.Setenv("RATE_LIMIT_TEST", v)
		if got := envFloat("RATE_LIMIT_TEST", 7); got != 7 {
			t.Fatalf("envFloat(%q) = %v，应当退回 7", v, got)
		}
	}
	t.Setenv("RATE_LIMIT_TEST", "2.5")
	if got := envFloat("RATE_LIMIT_TEST", 7); got != 2.5 {
		t.Fatalf("应当读到 2.5，实际 %v", got)
	}
}
