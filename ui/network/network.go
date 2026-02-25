// SPDX-FileContributor: FlameFlag <github@flameflag.dev>
//
// SPDX-License-Identifier: AGPLv3+

package network

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/asciimoo/hister/server/indexer"
	"github.com/asciimoo/hister/ui/model"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gorilla/websocket"
)

func ListenToWebSocket(wsChan chan tea.Msg, wsDone chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-wsChan:
			return msg
		case <-wsDone:
			return nil
		}
	}
}

func ConnectWebSocket(wsURL, origin string, wsChan chan tea.Msg, wsDone chan struct{}) tea.Cmd {
	return func() tea.Msg {
		header := http.Header{}
		header.Set("Origin", origin)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			return model.WsDisconnectedMsg{Err: err}
		}
		go func() {
			defer conn.Close()
			for {
				select {
				case <-wsDone:
					return
				default:
					_, data, err := conn.ReadMessage()
					if err != nil {
						select {
						case wsChan <- model.WsDisconnectedMsg{Err: err}:
						case <-wsDone:
						}
						return
					}
					var res *indexer.Results
					if err := json.Unmarshal(data, &res); err != nil {
						continue
					}
					if len(res.Documents) == 0 && len(res.History) == 0 {
						res = &indexer.Results{}
					}
					select {
					case wsChan <- model.ResultsMsg{Results: res}:
					case <-wsDone:
						return
					}
				}
			}
		}()
		return model.WsConnectedMsg{Conn: conn}
	}
}

func Search(conn *websocket.Conn, wsMu *sync.Mutex, wsReady bool, q model.SearchQuery) tea.Cmd {
	return func() tea.Msg {
		if !wsReady || conn == nil {
			return nil
		}
		b, err := json.Marshal(q)
		if err != nil {
			return nil
		}
		wsMu.Lock()
		conn.WriteMessage(websocket.TextMessage, b)
		wsMu.Unlock()
		return nil
	}
}

func FetchHistory(baseURL string) tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequest("GET", baseURL+"/api/history", nil)
		req.Header.Set("Origin", "hister://")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return model.HistoryFetchedMsg{Items: nil}
		}
		defer resp.Body.Close()
		var items []model.HistoryEntry
		json.NewDecoder(resp.Body).Decode(&items)
		return model.HistoryFetchedMsg{Items: items}
	}
}

func FetchRules(baseURL string) tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequest("GET", baseURL+"/api/rules", nil)
		req.Header.Set("Origin", "hister://")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return model.RulesFetchedMsg{}
		}
		defer resp.Body.Close()
		var data model.RulesResponse
		json.NewDecoder(resp.Body).Decode(&data)
		return model.RulesFetchedMsg{Data: data}
	}
}

func PostAdd(baseURL, u, title, text string) tea.Cmd {
	return func() tea.Msg {
		formData := url.Values{"url": {u}, "title": {title}, "text": {text}}
		req, _ := http.NewRequest("POST", baseURL+"/api/add", strings.NewReader(formData.Encode()))
		req.Header.Set("Origin", "hister://")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return model.AddResultMsg{Err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				msg = resp.Status
			}
			return model.AddResultMsg{Err: fmt.Errorf("%s", msg)}
		}
		return model.AddResultMsg{Err: nil}
	}
}

func PostRules(baseURL, skip, priority string) tea.Cmd {
	return func() tea.Msg {
		formData := url.Values{"skip": {skip}, "priority": {priority}}
		req, _ := http.NewRequest("POST", baseURL+"/api/rules", strings.NewReader(formData.Encode()))
		req.Header.Set("Origin", "hister://")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err := http.DefaultClient.Do(req)
		return model.RulesSavedMsg{Err: err}
	}
}

func PostAddAlias(baseURL, keyword, value string) tea.Cmd {
	return func() tea.Msg {
		formData := url.Values{"alias-keyword": {keyword}, "alias-value": {value}}
		req, _ := http.NewRequest("POST", baseURL+"/api/add_alias", strings.NewReader(formData.Encode()))
		req.Header.Set("Origin", "hister://")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err := http.DefaultClient.Do(req)
		return model.RulesSavedMsg{Err: err}
	}
}

func PostDeleteAlias(baseURL, alias string) tea.Cmd {
	return func() tea.Msg {
		formData := url.Values{"alias": {alias}}
		req, _ := http.NewRequest("POST", baseURL+"/api/delete_alias", strings.NewReader(formData.Encode()))
		req.Header.Set("Origin", "hister://")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err := http.DefaultClient.Do(req)
		return model.RulesSavedMsg{Err: err}
	}
}

func PostHistory(baseURL, queryStr, urlStr, title string) tea.Cmd {
	return func() tea.Msg {
		body := struct {
			URL   string `json:"url"`
			Title string `json:"title"`
			Query string `json:"query"`
		}{URL: urlStr, Title: title, Query: queryStr}
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", baseURL+"/api/history", strings.NewReader(string(data)))
		req.Header.Set("Origin", "hister://")
		req.Header.Set("Content-Type", "application/json")
		http.DefaultClient.Do(req)
		return nil
	}
}

func DeleteHistoryEntry(baseURL, queryStr, urlStr string) tea.Cmd {
	return func() tea.Msg {
		body := struct {
			URL    string `json:"url"`
			Query  string `json:"query"`
			Delete bool   `json:"delete"`
		}{URL: urlStr, Query: queryStr, Delete: true}
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", baseURL+"/api/history", strings.NewReader(string(data)))
		req.Header.Set("Origin", "hister://")
		req.Header.Set("Content-Type", "application/json")
		http.DefaultClient.Do(req)
		return FetchHistory(baseURL)()
	}
}

func DeleteURL(baseURL, u string) tea.Cmd {
	return func() tea.Msg {
		formData := url.Values{"url": {u}}
		req, _ := http.NewRequest("POST", baseURL+"/api/delete", strings.NewReader(formData.Encode()))
		req.Header.Set("Origin", "hister://")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		http.DefaultClient.Do(req)
		// Return nil — the caller will trigger a search afterward.
		return nil
	}
}
