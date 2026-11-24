// Package notify sends alerts to the admin. Telegram is the main channel;
// button presses come back by long polling, so no public webhook is needed.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Button is one inline button: either a callback (Data) or a link (URL).
type Button struct {
	Text string
	Data string // callback payload, at most 64 bytes
	URL  string
}

// Telegram talks to the Bot API.
type Telegram struct {
	token   string
	chatIDs []int64
	client  *http.Client
	// callbacks is true while getUpdates works; false if another program owns
	// the bot's updates (a webhook or a second poller), so links are used instead.
	callbacks atomic.Bool
	// OnCallback handles a button press; it returns the text shown on the message.
	OnCallback func(data string, fromID int64) (string, error)
}

// NewTelegram returns nil when the token or chat IDs are missing.
func NewTelegram(token, chatIDs string) *Telegram {
	if token == "" || chatIDs == "" {
		return nil
	}
	t := &Telegram{token: token, client: &http.Client{Timeout: 70 * time.Second, Transport: &http.Transport{MaxIdleConns: 1, IdleConnTimeout: 90 * time.Second}}}
	for _, s := range strings.Split(chatIDs, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			t.chatIDs = append(t.chatIDs, id)
		}
	}
	if len(t.chatIDs) == 0 {
		return nil
	}
	return t // callbacks stay off until Poll confirms no webhook owns the bot
}

// CallbacksWork reports whether Approve/Reject buttons can be callback buttons.
func (t *Telegram) CallbacksWork() bool { return t != nil && t.callbacks.Load() }

type apiResp struct {
	OK          bool            `json:"ok"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (t *Telegram) call(ctx context.Context, method string, body any) (json.RawMessage, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+t.token+"/"+method, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := t.client.Do(req)
	if err != nil {
		// Never log the URL: it contains the token.
		return nil, fmt.Errorf("telegram %s: request failed", method)
	}
	defer res.Body.Close()
	var r apiResp
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("telegram %s: bad response", method)
	}
	if !r.OK {
		return nil, &APIError{Code: r.ErrorCode, Desc: r.Description}
	}
	return r.Result, nil
}

type APIError struct {
	Code int
	Desc string
}

func (e *APIError) Error() string { return fmt.Sprintf("telegram: %d %s", e.Code, e.Desc) }

// Send posts text with optional buttons (one row per slice) to every admin chat.
func (t *Telegram) Send(ctx context.Context, text string, rows ...[]Button) error {
	if t == nil {
		return nil
	}
	var kb [][]map[string]string
	for _, row := range rows {
		var r []map[string]string
		for _, b := range row {
			switch {
			case b.Data != "" && t.CallbacksWork():
				r = append(r, map[string]string{"text": b.Text, "callback_data": b.Data})
			case b.URL != "":
				r = append(r, map[string]string{"text": b.Text, "url": b.URL})
			}
		}
		if len(r) > 0 {
			kb = append(kb, r)
		}
	}
	var firstErr error
	for _, id := range t.chatIDs {
		body := map[string]any{"chat_id": id, "text": text, "disable_web_page_preview": true}
		if len(kb) > 0 {
			body["reply_markup"] = map[string]any{"inline_keyboard": kb}
		}
		if _, err := t.call(ctx, "sendMessage", body); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (t *Telegram) isAdmin(id int64) bool {
	for _, c := range t.chatIDs {
		if c == id {
			return true
		}
	}
	return false
}

// Poll long-polls getUpdates until ctx ends. Idle cost: one open HTTPS request.
func (t *Telegram) Poll(ctx context.Context) {
	if t == nil {
		return
	}
	// A webhook (e.g. n8n using the same bot) receives every button press,
	// so Printty must not send callback buttons or poll.
	if res, err := t.call(ctx, "getWebhookInfo", map[string]any{}); err == nil {
		var info struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(res, &info) == nil && info.URL != "" {
			t.callbacks.Store(false)
			slog.Info("telegram: this bot has a webhook (another app uses it); alert buttons will open Printty instead. Use a dedicated bot to approve inside Telegram.")
			return
		}
	}
	t.callbacks.Store(true)
	offset := 0
	backoff := time.Second
	for ctx.Err() == nil {
		res, err := t.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 50, "allowed_updates": []string{"callback_query"}})
		if err != nil {
			if ae, ok := err.(*APIError); ok && ae.Code == 409 {
				// Someone else owns this bot's updates (a webhook, e.g. n8n).
				t.callbacks.Store(false)
				slog.Warn("telegram: bot updates are taken by another program; using link buttons instead", "detail", ae.Desc)
				return
			}
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram poll failed", "err", err)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			backoff = min(backoff*2, 5*time.Minute)
			continue
		}
		backoff = time.Second
		var updates []struct {
			UpdateID int `json:"update_id"`
			Callback *struct {
				ID   string `json:"id"`
				From struct {
					ID int64 `json:"id"`
				} `json:"from"`
				Data    string `json:"data"`
				Message *struct {
					MessageID int    `json:"message_id"`
					Text      string `json:"text"`
					Chat      struct {
						ID int64 `json:"id"`
					} `json:"chat"`
				} `json:"message"`
			} `json:"callback_query"`
		}
		if err := json.Unmarshal(res, &updates); err != nil {
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			cb := u.Callback
			if cb == nil {
				continue
			}
			answer := "Not allowed"
			chatOK := cb.Message != nil && t.isAdmin(cb.Message.Chat.ID)
			if (t.isAdmin(cb.From.ID) || chatOK) && t.OnCallback != nil {
				out, err := t.OnCallback(cb.Data, cb.From.ID)
				if err != nil {
					answer = err.Error()
				} else {
					answer = out
					if cb.Message != nil {
						t.call(ctx, "editMessageText", map[string]any{
							"chat_id": cb.Message.Chat.ID, "message_id": cb.Message.MessageID,
							"text": cb.Message.Text + "\n\n" + out,
						})
					}
				}
			}
			t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": cb.ID, "text": answer})
		}
	}
}

// Test sends a test message.
func (t *Telegram) Test(ctx context.Context, from string) error {
	return t.Send(ctx, "Printty test message. Alerts are working ("+from+").")
}

// QueryEscape is re-exported for building links.
var QueryEscape = url.QueryEscape
