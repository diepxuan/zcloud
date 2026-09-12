package api

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/diepxuan/zcloud/internal/core"
	"github.com/diepxuan/zcloud/internal/store"
)

// FriendsWorker định kỳ fetch GetFriends cho từng account enabled và UPSERT
// vào bảng contacts. KHÔNG xoá contact cũ — strategy này giữ data khi Zalo
// rate-limit (429) hoặc bug upstream trả thiếu.
//
// Schedule (theo yêu cầu của Sếp Duc Tran):
//   T=0          acc1 sync
//   T=3s         acc2 sync
//   T=6s         acc3 sync
//   ...
//   T=N*3s       accN sync
//   T=interval   lặp lại acc1 (interval mặc định 1 phút)
type FriendsWorker struct {
	store           *store.Store
	logger          *log.Logger
	interval        time.Duration // cycle giữa 2 lần sync cùng account (mặc định 1 phút)
	staggerInterval time.Duration // delay giữa các account trong cùng cycle (mặc định 3s)

	stop chan struct{}
	done chan struct{}
}

const (
	defaultFriendsInterval        = 1 * time.Minute
	defaultFriendsStaggerInterval = 3 * time.Second
)

func NewFriendsWorker(st *store.Store, logger *log.Logger, interval, stagger time.Duration) *FriendsWorker {
	if interval <= 0 {
		interval = defaultFriendsInterval
	}
	if stagger < 0 {
		stagger = defaultFriendsStaggerInterval
	}
	return &FriendsWorker{
		store:           st,
		logger:          logger,
		interval:        interval,
		staggerInterval: stagger,
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
}

func (w *FriendsWorker) Start(ctx context.Context) {
	go w.loop(ctx)
}

func (w *FriendsWorker) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
}

func (w *FriendsWorker) loop(ctx context.Context) {
	defer close(w.done)
	// Sync ngay khi start để UI có data ngay sau restart.
	w.tick(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick chạy 1 cycle: sync lần lượt các account enabled, delay staggerInterval
// giữa mỗi account. Nếu ctx huỷ giữa chừng, thoát sớm.
func (w *FriendsWorker) tick(ctx context.Context) {
	accounts, err := w.store.ListAccounts(0, true) // enabledOnly=true
	if err != nil {
		w.logger.Printf("friends-worker: list accounts: %v", err)
		return
	}
	if len(accounts) == 0 {
		return
	}

	w.logger.Printf("friends-worker: cycle start — %d enabled account(s)", len(accounts))

	for i, acc := range accounts {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				return
			case <-time.After(w.staggerInterval):
			}
		}
		w.syncOne(ctx, acc.ID, acc.DisplayName)
	}
}

// syncOne fetch GetFriends cho 1 account và upsert từng contact vào DB.
func (w *FriendsWorker) syncOne(ctx context.Context, accountID, label string) {
	sessRec, err := w.store.GetActiveSession(accountID)
	if err != nil || sessRec == nil {
		w.logger.Printf("friends-worker: %s (%s): no active session, skip", label, accountID)
		return
	}
	var cookies map[string]string
	json.Unmarshal([]byte(sessRec.Cookies), &cookies)
	session := &core.Session{
		Cookies: cookies, SecretKey: sessRec.SecretKey, IMEI: sessRec.IMEI,
		UserAgent: sessRec.UserAgent, APIType: sessRec.APIType, APIVersion: sessRec.APIVersion,
	}
	var serviceMap map[string][]string
	if json.Unmarshal([]byte(sessRec.ServiceMap), &serviceMap) == nil {
		session.ServiceMap = serviceMap
	}
	client := core.NewClient(session)
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	users, err := client.GetFriends(callCtx)
	if err != nil {
		// KHÔNG xoá DB khi upstream lỗi — giữ contact cũ.
		w.logger.Printf("friends-worker: %s (%s): zalo error: %v (giữ DB cũ)", label, accountID, err)
		return
	}

	upserted := 0
	for _, u := range users {
		if err := w.store.UpsertContact(accountID, u.ID, u.Name, u.Avatar); err != nil {
			w.logger.Printf("friends-worker: %s: upsert %s err: %v", label, u.ID, err)
			continue
		}
		upserted++
	}
	w.logger.Printf("friends-worker: %s (%s): upserted %d contact(s)", label, accountID, upserted)
}
