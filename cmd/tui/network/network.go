// SPDX-FileContributor: 4evy <git@evy.pink>
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package network

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/asciimoo/nogon/client"
	"github.com/asciimoo/nogon/cmd/tui/model"

	tea "charm.land/bubbletea/v2"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
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

func ConnectWebSocket(wsURL, origin, token string, wsChan chan tea.Msg, wsDone chan struct{}) tea.Cmd {
	return func() tea.Msg {
		header := http.Header{}
		header.Set("Origin", origin)
		if token != "" {
			header.Set("X-Access-Token", token)
		}
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
		if resp != nil && resp.Body != nil {
			defer func() {
				if cerr := resp.Body.Close(); cerr != nil {
					log.Warn().Err(cerr).Msg("failed to close websocket response body")
				}
			}()
		}
		if err != nil {
			return model.WsDisconnectedMsg{Err: err}
		}
		go func() {
			defer func() {
				if cerr := conn.Close(); cerr != nil {
					log.Warn().Err(cerr).Msg("failed to close websocket connection")
				}
			}()
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
					res, err := decodeResults(data)
					if err != nil {
						continue
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

func decodeResults(data []byte) (*client.SearchResults, error) {
	var results client.SearchResults
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return &results, nil
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
		if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
			log.Warn().Err(err).Msg("failed to write websocket message")
		}
		wsMu.Unlock()
		return nil
	}
}
