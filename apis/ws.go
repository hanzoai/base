package apis

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/crdt"
	"github.com/hanzoai/base/tools/router"
	"github.com/hanzoai/base/tools/security"
)

// WebSocket opcodes per RFC 6455.
const (
	wsOpText   = 1
	wsOpBinary = 2
	wsOpClose  = 8
	wsOpPing   = 9
	wsOpPong   = 10
)

// WebSocket close codes.
const (
	wsCloseNormal       = 1000
	wsCloseGoingAway    = 1001
	wsCloseProtocolErr  = 1002
	wsCloseUnsupported  = 1003
	wsCloseAbnormal     = 1006
	wsCloseInvalidData  = 1007
	wsClosePolicyViolation = 1008
	wsCloseTooLarge     = 1009
)

// wsGUID is the magic GUID from RFC 6455 section 4.2.2.
const wsGUID = "258EAFA5-E914-47DA-95CA-5AB0A4A2E698"

// WebSocket message types for the realtime protocol.
const (
	WSMsgAuthenticate = "authenticate"
	WSMsgSubscribe    = "subscribe"
	WSMsgUnsubscribe  = "unsubscribe"
	WSMsgMutation     = "mutation"
	WSMsgPresence     = "presence"
	WSMsgCRDTSync     = "crdt_sync"
	WSMsgPing         = "ping"

	WSMsgTransition     = "transition"
	WSMsgMutationResp   = "mutation_response"
	WSMsgAuthResult     = "auth_result"
	WSMsgPresenceUpdate = "presence_update"
	WSMsgCRDTUpdate     = "crdt_update"
	WSMsgError          = "error"
	WSMsgPong           = "pong"
	WSMsgConnected      = "connected"
)

// WSMessage is the envelope for all WebSocket protocol messages.
type WSMessage struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WSAuthPayload is sent by the client to authenticate.
type WSAuthPayload struct {
	Token string `json:"token"`
}

// WSSubscribePayload is sent by the client to subscribe to a query.
type WSSubscribePayload struct {
	Queries []string `json:"queries"`
}

// WSPresencePayload is sent by the client for presence updates.
type WSPresencePayload struct {
	Action  string         `json:"action"` // "join", "leave", "update"
	Channel string         `json:"channel"`
	State   map[string]any `json:"state,omitempty"`
}

// WSCRDTSyncPayload wraps a CRDT sync message.
type WSCRDTSyncPayload struct {
	DocID       string               `json:"docId"`
	Type        string               `json:"type"` // sync_step1, sync_step2, sync_update
	StateVector crdt.StateVersion    `json:"stateVector,omitempty"`
	Ops         []crdt.Operation     `json:"ops,omitempty"`
}

// wsConn wraps a hijacked net.Conn for WebSocket framing.
type wsConn struct {
	conn   net.Conn
	br     *bufio.Reader
	bw     *bufio.Writer
	mu     sync.Mutex // protects writes
	closed bool
}

// wsClient tracks state for a single WebSocket connection.
type wsClient struct {
	id            string
	conn          *wsConn
	auth          *core.Record
	subscriptions map[string]bool
	mu            sync.RWMutex
	cancelCtx     context.CancelFunc
}

// WSHub manages all WebSocket clients and routes messages.
type WSHub struct {
	mu       sync.RWMutex
	clients  map[string]*wsClient
	presence *PresenceManager
	sync     *crdt.SyncManager
	app      core.App
}

// NewWSHub creates a new WebSocket hub.
func NewWSHub(app core.App) *WSHub {
	hub := &WSHub{
		clients:  make(map[string]*wsClient),
		presence: NewPresenceManager(),
		app:      app,
	}
	hub.sync = crdt.NewSyncManager(hub.broadcastCRDT)
	return hub
}

// broadcastCRDT is the callback for SyncManager to broadcast CRDT ops.
func (h *WSHub) broadcastCRDT(docID string, excludeClient string, msg []byte) {
	payload := json.RawMessage(msg)
	envelope := WSMessage{
		Type:    WSMsgCRDTUpdate,
		Payload: payload,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for id, client := range h.clients {
		if id == excludeClient {
			continue
		}
		// broadcast to clients subscribed to "crdt:<docID>"
		client.mu.RLock()
		subscribed := client.subscriptions["crdt:"+docID]
		client.mu.RUnlock()
		if subscribed {
			go client.conn.writeText(data)
		}
	}
}

// Presence returns the hub's PresenceManager.
func (h *WSHub) Presence() *PresenceManager {
	return h.presence
}

// SyncManager returns the hub's CRDT SyncManager.
func (h *WSHub) SyncManager() *crdt.SyncManager {
	return h.sync
}

// register adds a client to the hub.
func (h *WSHub) register(client *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client.id] = client
}

// unregister removes a client and cleans up presence.
func (h *WSHub) unregister(clientID string) {
	h.mu.Lock()
	client, ok := h.clients[clientID]
	delete(h.clients, clientID)
	h.mu.Unlock()

	if ok {
		h.presence.LeaveAll(clientID)
		client.conn.close(wsCloseGoingAway, "")
	}
}

// bindWSApi registers the WebSocket endpoint.
func bindWSApi(app core.App, rg *router.RouterGroup[*core.RequestEvent], hub *WSHub) {
	sub := rg.Group("/realtime")
	sub.GET("/ws", wsUpgradeHandler(app, hub)).Bind(SkipSuccessActivityLog())
}

// wsUpgradeHandler returns the handler that upgrades HTTP to WebSocket.
func wsUpgradeHandler(app core.App, hub *WSHub) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		// validate WebSocket upgrade headers
		if !strings.EqualFold(e.Request.Header.Get("Upgrade"), "websocket") {
			return e.BadRequestError("Expected WebSocket upgrade", nil)
		}
		if !headerContains(e.Request.Header, "Connection", "upgrade") {
			return e.BadRequestError("Expected Connection: upgrade", nil)
		}
		wsKey := e.Request.Header.Get("Sec-WebSocket-Key")
		if wsKey == "" {
			return e.BadRequestError("Missing Sec-WebSocket-Key", nil)
		}
		if e.Request.Header.Get("Sec-WebSocket-Version") != "13" {
			return e.BadRequestError("Unsupported WebSocket version", nil)
		}

		// compute accept key
		acceptKey := computeAcceptKey(wsKey)

		// hijack the connection
		hj, ok := e.Response.(http.Hijacker)
		if !ok {
			return e.InternalServerError("Server does not support WebSocket hijack", nil)
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			return e.InternalServerError("WebSocket hijack failed", err)
		}

		// send upgrade response
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"
		if _, err := brw.WriteString(resp); err != nil {
			conn.Close()
			return nil
		}
		if err := brw.Flush(); err != nil {
			conn.Close()
			return nil
		}

		ws := &wsConn{
			conn: conn,
			br:   brw.Reader,
			bw:   brw.Writer,
		}

		clientID := security.RandomString(40)
		ctx, cancel := context.WithCancel(context.Background())

		client := &wsClient{
			id:            clientID,
			conn:          ws,
			auth:          e.Auth, // may be nil (authenticate later)
			subscriptions: make(map[string]bool),
			cancelCtx:     cancel,
		}

		// check for token in query param
		if token := e.Request.URL.Query().Get("token"); token != "" && client.auth == nil {
			record, authErr := app.FindAuthRecordByToken(token, core.TokenTypeAuth)
			if authErr == nil && record != nil {
				client.auth = record
			}
		}

		hub.register(client)

		app.Logger().Debug("WebSocket connection established",
			slog.String("clientId", clientID))

		// send connected message
		connMsg := WSMessage{
			Type:    WSMsgConnected,
			Payload: json.RawMessage(`{"clientId":"` + clientID + `"}`),
		}
		if data, err := json.Marshal(connMsg); err == nil {
			ws.writeText(data)
		}

		// handle messages in a goroutine (we already hijacked)
		go func() {
			defer hub.unregister(clientID)
			handleWSMessages(ctx, app, hub, client)
		}()

		// block until context is cancelled (connection closed)
		<-ctx.Done()
		return nil
	}
}

// handleWSMessages reads and processes WebSocket messages.
func handleWSMessages(ctx context.Context, app core.App, hub *WSHub, client *wsClient) {
	defer client.cancelCtx()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// set read deadline for idle timeout
		client.conn.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		opcode, payload, err := client.conn.readMessage()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				app.Logger().Debug("WebSocket read error",
					slog.String("clientId", client.id),
					slog.String("error", err.Error()))
			}
			return
		}

		switch opcode {
		case wsOpText:
			processWSMessage(app, hub, client, payload)
		case wsOpBinary:
			processWSMessage(app, hub, client, payload)
		case wsOpPing:
			client.conn.writePong(payload)
		case wsOpClose:
			return
		}
	}
}

// processWSMessage dispatches a single parsed WebSocket message.
func processWSMessage(app core.App, hub *WSHub, client *wsClient, raw []byte) {
	var msg WSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		sendWSError(client, "", "invalid message format")
		return
	}

	switch msg.Type {
	case WSMsgAuthenticate:
		handleWSAuth(app, client, msg)
	case WSMsgSubscribe:
		handleWSSubscribe(client, msg)
	case WSMsgUnsubscribe:
		handleWSUnsubscribe(client, msg)
	case WSMsgPresence:
		handleWSPresence(hub, client, msg)
	case WSMsgCRDTSync:
		handleWSCRDTSync(hub, client, msg)
	case WSMsgPing:
		resp := WSMessage{Type: WSMsgPong, ID: msg.ID}
		if data, err := json.Marshal(resp); err == nil {
			client.conn.writeText(data)
		}
	default:
		sendWSError(client, msg.ID, "unknown message type: "+msg.Type)
	}
}

// handleWSAuth authenticates the client via JWT token.
func handleWSAuth(app core.App, client *wsClient, msg WSMessage) {
	var payload WSAuthPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		sendWSError(client, msg.ID, "invalid auth payload")
		return
	}

	record, err := app.FindAuthRecordByToken(payload.Token, core.TokenTypeAuth)
	if err != nil || record == nil {
		resp := WSMessage{
			Type:    WSMsgAuthResult,
			ID:      msg.ID,
			Payload: json.RawMessage(`{"success":false,"error":"invalid token"}`),
		}
		if data, err := json.Marshal(resp); err == nil {
			client.conn.writeText(data)
		}
		return
	}

	client.mu.Lock()
	client.auth = record
	client.mu.Unlock()

	resp := WSMessage{
		Type:    WSMsgAuthResult,
		ID:      msg.ID,
		Payload: json.RawMessage(`{"success":true}`),
	}
	if data, err := json.Marshal(resp); err == nil {
		client.conn.writeText(data)
	}
}

// handleWSSubscribe adds subscriptions.
func handleWSSubscribe(client *wsClient, msg WSMessage) {
	var payload WSSubscribePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		sendWSError(client, msg.ID, "invalid subscribe payload")
		return
	}

	client.mu.Lock()
	for _, q := range payload.Queries {
		client.subscriptions[q] = true
	}
	client.mu.Unlock()

	resp := WSMessage{
		Type:    WSMsgTransition,
		ID:      msg.ID,
		Payload: json.RawMessage(`{"status":"subscribed"}`),
	}
	if data, err := json.Marshal(resp); err == nil {
		client.conn.writeText(data)
	}
}

// handleWSUnsubscribe removes subscriptions.
func handleWSUnsubscribe(client *wsClient, msg WSMessage) {
	var payload WSSubscribePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		sendWSError(client, msg.ID, "invalid unsubscribe payload")
		return
	}

	client.mu.Lock()
	for _, q := range payload.Queries {
		delete(client.subscriptions, q)
	}
	client.mu.Unlock()

	resp := WSMessage{
		Type:    WSMsgTransition,
		ID:      msg.ID,
		Payload: json.RawMessage(`{"status":"unsubscribed"}`),
	}
	if data, err := json.Marshal(resp); err == nil {
		client.conn.writeText(data)
	}
}

// handleWSPresence processes presence messages.
func handleWSPresence(hub *WSHub, client *wsClient, msg WSMessage) {
	var payload WSPresencePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		sendWSError(client, msg.ID, "invalid presence payload")
		return
	}

	switch payload.Action {
	case "join":
		hub.presence.Join(payload.Channel, client.id, payload.State)
	case "leave":
		hub.presence.Leave(payload.Channel, client.id)
	case "update":
		hub.presence.Update(payload.Channel, client.id, payload.State)
	default:
		sendWSError(client, msg.ID, "unknown presence action: "+payload.Action)
		return
	}

	// broadcast presence to channel subscribers
	entries := hub.presence.GetPresence(payload.Channel)
	presenceData, _ := json.Marshal(map[string]any{
		"channel":  payload.Channel,
		"presence": entries,
	})
	broadcastMsg := WSMessage{
		Type:    WSMsgPresenceUpdate,
		Payload: json.RawMessage(presenceData),
	}
	broadcastData, _ := json.Marshal(broadcastMsg)

	hub.mu.RLock()
	for _, c := range hub.clients {
		c.mu.RLock()
		subscribed := c.subscriptions["presence:"+payload.Channel]
		c.mu.RUnlock()
		if subscribed {
			go c.conn.writeText(broadcastData)
		}
	}
	hub.mu.RUnlock()
}

// handleWSCRDTSync processes CRDT synchronization messages.
func handleWSCRDTSync(hub *WSHub, client *wsClient, msg WSMessage) {
	var payload WSCRDTSyncPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		sendWSError(client, msg.ID, "invalid crdt_sync payload")
		return
	}

	// auto-subscribe client to CRDT doc updates
	client.mu.Lock()
	client.subscriptions["crdt:"+payload.DocID] = true
	client.mu.Unlock()

	// ensure document exists
	hub.sync.GetOrCreateDocument(payload.DocID, "server")

	// build the sync message for the SyncManager
	syncMsg := crdt.SyncMessage{
		Type:        payload.Type,
		DocID:       payload.DocID,
		ClientID:    client.id,
		StateVector: payload.StateVector,
		Ops:         payload.Ops,
	}
	syncData, err := json.Marshal(syncMsg)
	if err != nil {
		sendWSError(client, msg.ID, "marshal sync message failed")
		return
	}

	resp, err := hub.sync.HandleSync(client.id, syncData)
	if err != nil {
		sendWSError(client, msg.ID, "sync error: "+err.Error())
		return
	}

	if resp != nil {
		envelope := WSMessage{
			Type:    WSMsgCRDTUpdate,
			ID:      msg.ID,
			Payload: json.RawMessage(resp),
		}
		if data, err := json.Marshal(envelope); err == nil {
			client.conn.writeText(data)
		}
	}
}

// sendWSError sends an error message back to the client.
func sendWSError(client *wsClient, msgID string, errMsg string) {
	payload, _ := json.Marshal(map[string]string{"error": errMsg})
	resp := WSMessage{
		Type:    WSMsgError,
		ID:      msgID,
		Payload: json.RawMessage(payload),
	}
	if data, err := json.Marshal(resp); err == nil {
		client.conn.writeText(data)
	}
}

// -------------------------------------------------------------------
// Pure WebSocket framing (RFC 6455) - no external dependencies
// -------------------------------------------------------------------

// computeAcceptKey implements the Sec-WebSocket-Accept key derivation.
func computeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// headerContains checks if a comma-separated header contains a value (case-insensitive).
func headerContains(h http.Header, key, value string) bool {
	for _, v := range h[http.CanonicalHeaderKey(key)] {
		for _, s := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(s), value) {
				return true
			}
		}
	}
	return false
}

// readMessage reads a single WebSocket frame (handles fragmentation for text/binary).
func (ws *wsConn) readMessage() (opcode byte, payload []byte, err error) {
	for {
		var fin bool
		var op byte
		var data []byte

		fin, op, data, err = ws.readFrame()
		if err != nil {
			return 0, nil, err
		}

		// handle control frames inline
		if op >= 0x8 {
			return op, data, nil
		}

		if opcode == 0 {
			opcode = op
		}
		payload = append(payload, data...)

		if fin {
			return opcode, payload, nil
		}
	}
}

// readFrame reads a single WebSocket frame.
func (ws *wsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	// read first 2 bytes
	header := make([]byte, 2)
	if _, err = io.ReadFull(ws.br, header); err != nil {
		return false, 0, nil, err
	}

	fin = (header[0] & 0x80) != 0
	opcode = header[0] & 0x0F
	masked := (header[1] & 0x80) != 0
	length := uint64(header[1] & 0x7F)

	// extended payload length
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err = io.ReadFull(ws.br, ext); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err = io.ReadFull(ws.br, ext); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}

	// sanity limit: 16MB
	if length > 16*1024*1024 {
		return false, 0, nil, fmt.Errorf("frame too large: %d bytes", length)
	}

	// read masking key
	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(ws.br, maskKey[:]); err != nil {
			return false, 0, nil, err
		}
	}

	// read payload
	payload = make([]byte, length)
	if length > 0 {
		if _, err = io.ReadFull(ws.br, payload); err != nil {
			return false, 0, nil, err
		}
	}

	// unmask
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	// validate UTF-8 for text frames
	if opcode == wsOpText && fin && !utf8.Valid(payload) {
		return false, 0, nil, fmt.Errorf("invalid UTF-8 in text frame")
	}

	return fin, opcode, payload, nil
}

// writeText writes a text message frame.
func (ws *wsConn) writeText(data []byte) error {
	return ws.writeFrame(wsOpText, data)
}

// writePong writes a pong control frame.
func (ws *wsConn) writePong(data []byte) error {
	return ws.writeFrame(wsOpPong, data)
}

// close sends a close frame and closes the connection.
func (ws *wsConn) close(code uint16, reason string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.closed {
		return nil
	}
	ws.closed = true

	// build close payload
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	copy(payload[2:], reason)

	// write close frame (best effort)
	ws.writeFrameLocked(wsOpClose, payload)
	return ws.conn.Close()
}

// writeFrame writes a single WebSocket frame (server->client, unmasked).
func (ws *wsConn) writeFrame(opcode byte, data []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.closed {
		return fmt.Errorf("connection closed")
	}
	return ws.writeFrameLocked(opcode, data)
}

// writeFrameLocked writes a frame assuming the mutex is held.
func (ws *wsConn) writeFrameLocked(opcode byte, data []byte) error {
	// FIN + opcode
	ws.bw.WriteByte(0x80 | opcode)

	// payload length (server frames are unmasked)
	length := len(data)
	switch {
	case length <= 125:
		ws.bw.WriteByte(byte(length))
	case length <= 65535:
		ws.bw.WriteByte(126)
		lenBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(lenBytes, uint16(length))
		ws.bw.Write(lenBytes)
	default:
		ws.bw.WriteByte(127)
		lenBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(lenBytes, uint64(length))
		ws.bw.Write(lenBytes)
	}

	// payload
	if length > 0 {
		ws.bw.Write(data)
	}

	return ws.bw.Flush()
}
