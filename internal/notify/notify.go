package notify

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pushoverAPI = "https://api.pushover.net/1/messages.json"

// Priority levels mirror Pushover's API
const (
	PriorityLow    = -1
	PriorityNormal = 0
	PriorityHigh   = 1
	// Emergency (2) requires retry/expire — we use High for simplicity
)

type Notifier struct {
	appKey  string
	userKey string
	client  *http.Client
}

func New(appKey, userKey string) *Notifier {
	return &Notifier{
		appKey:  appKey,
		userKey: userKey,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// SendEmergency sends a high-priority alert for data-loss risk situations
func (n *Notifier) SendEmergency(title, message string) {
	n.send(title, message, PriorityHigh)
}

// SendInfo sends a normal-priority informational message
func (n *Notifier) SendInfo(title, message string) {
	n.send(title, message, PriorityNormal)
}

// SendLow sends a low-priority report (weekly/monthly summaries)
func (n *Notifier) SendLow(title, message string) {
	n.send(title, message, PriorityLow)
}

func (n *Notifier) send(title, message string, priority int) {
	data := url.Values{
		"token":    {n.appKey},
		"user":     {n.userKey},
		"title":    {title},
		"message":  {message},
		"priority": {fmt.Sprintf("%d", priority)},
	}

	resp, err := n.client.Post(pushoverAPI, "application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()))
	if err != nil {
		log.Printf("[notify] failed to send pushover: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[notify] pushover returned %d: %s", resp.StatusCode, body)
		return
	}

	log.Printf("[notify] sent: [%s] %s", title, message)
}
