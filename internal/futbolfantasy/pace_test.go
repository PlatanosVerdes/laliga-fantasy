package futbolfantasy

import (
	"sync"
	"testing"
	"time"
)

func TestAReadSomeoneWaitsOnGoesBeforeTheBackgroundQueue(t *testing.T) {
	lastRequest = time.Now()
	var mu sync.Mutex
	var order []string
	var all sync.WaitGroup
	take := func(name string, urgent bool) {
		defer all.Done()
		pace(urgent)
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
	}
	for _, name := range []string{"fill-1", "fill-2", "fill-3"} {
		all.Add(1)
		go take(name, false)
	}
	time.Sleep(50 * time.Millisecond)
	all.Add(1)
	go take("card", true)
	all.Wait()
	if len(order) != 4 || order[1] != "card" && order[0] != "card" {
		t.Errorf("the card waited behind the fill: %v", order)
	}
}
