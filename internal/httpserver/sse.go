package httpserver

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/linuxsuren/message-hub/internal/model"
)

// sseHub 维护全部 SSE 订阅者，按租户定向分发消息。
type sseHub struct {
	mu   sync.Mutex
	subs map[uint64]*subscriber
	next uint64
}

type subscriber struct {
	tenantID uint64
	ch       chan []byte
}

func newSSEHub() *sseHub {
	return &sseHub{subs: map[uint64]*subscriber{}}
}

func (h *sseHub) subscribe(tenantID uint64) (uint64, *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	sub := &subscriber{tenantID: tenantID, ch: make(chan []byte, 16)}
	h.subs[id] = sub
	return id, sub
}

func (h *sseHub) unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, id)
}

// publish 向「同租户订阅者 + 全体订阅者（公开消息）」推送消息帧；慢消费者丢帧不阻塞。
func (h *sseHub) publish(msg *model.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("SSE 序列化消息失败: %v", err)
		return
	}
	frame := []byte("event: message\ndata: " + string(data) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subs {
		if sub.tenantID == msg.TenantID || msg.Public {
			select {
			case sub.ch <- frame:
			default:
			}
		}
	}
}

// handleStream GET /api/v1/stream：SSE 实时推送本人租户与公开消息。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前连接不支持流式响应")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")

	user := s.currentUser(r)
	id, sub := s.hub.subscribe(user.TenantID)
	defer s.hub.unsubscribe(id)

	fmt.Fprint(w, "retry: 5000\n\n")
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-sub.ch:
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {\"ts\":%d}\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}
