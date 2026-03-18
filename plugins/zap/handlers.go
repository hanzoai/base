package zap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hanzoai/base/core"
	zaplib "github.com/luxfi/zap"
)

// ZAP message types for Base operations.
const (
	MsgTypeCollections uint16 = 100
	MsgTypeRecords     uint16 = 101
	MsgTypeAuth        uint16 = 102
	MsgTypeRealtime    uint16 = 103
)

// ZAP field offsets (matching orm/db/zap.go protocol).
const (
	fieldPath   = 4  // Text: request path
	fieldBody   = 12 // Bytes: JSON body
	respStatus  = 0  // Uint32: HTTP-style status
	respBody    = 4  // Bytes: response JSON
)

// handler wraps a Base app to handle ZAP messages.
type handler struct {
	app    core.App
	logger *slog.Logger
	node   *zaplib.Node

	// Realtime subscriptions: peer -> set of collection IDs/names
	subsMu sync.RWMutex
	subs   map[string]map[string]struct{}
}

func newHandler(app core.App, logger *slog.Logger) *handler {
	return &handler{
		app:    app,
		logger: logger,
		subs:   make(map[string]map[string]struct{}),
	}
}

// setNode sets the ZAP node reference for push notifications.
func (h *handler) setNode(node *zaplib.Node) {
	h.node = node
}

// handleCollections handles collection CRUD via ZAP.
func (h *handler) handleCollections(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
	root := msg.Root()
	path := root.Text(fieldPath)
	body := root.Bytes(fieldBody)

	h.logger.Debug("zap: collections", "path", path, "from", from, "bodyLen", len(body))

	var req map[string]interface{}
	if len(body) > 0 {
		json.Unmarshal(body, &req)
	}

	var result interface{}
	var err error

	switch path {
	case "/list":
		collections, findErr := h.app.FindAllCollections()
		if findErr != nil {
			return h.errorResponse(500, findErr.Error())
		}
		result = collections

	case "/get":
		name, _ := req["name"].(string)
		if name == "" {
			return h.errorResponse(400, "collection name required")
		}
		col, findErr := h.app.FindCollectionByNameOrId(name)
		if findErr != nil {
			return h.errorResponse(404, "collection not found")
		}
		result = col

	default:
		return h.errorResponse(400, fmt.Sprintf("unknown collections path: %s", path))
	}

	if err != nil {
		return h.errorResponse(500, err.Error())
	}
	return h.jsonResponse(200, result)
}

// handleRecords handles record CRUD via ZAP.
func (h *handler) handleRecords(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
	root := msg.Root()
	path := root.Text(fieldPath)
	body := root.Bytes(fieldBody)

	h.logger.Debug("zap: records", "path", path, "from", from)

	var req map[string]interface{}
	if len(body) > 0 {
		json.Unmarshal(body, &req)
	}

	collection, _ := req["collection"].(string)
	if collection == "" {
		return h.errorResponse(400, "collection required")
	}

	col, err := h.app.FindCollectionByNameOrId(collection)
	if err != nil {
		return h.errorResponse(404, "collection not found")
	}

	switch path {
	case "/list":
		records, findErr := h.app.FindAllRecords(col)
		if findErr != nil {
			return h.errorResponse(500, findErr.Error())
		}
		return h.jsonResponse(200, records)

	case "/get":
		id, _ := req["id"].(string)
		if id == "" {
			return h.errorResponse(400, "record id required")
		}
		record, findErr := h.app.FindRecordById(col, id)
		if findErr != nil {
			return h.errorResponse(404, "record not found")
		}
		return h.jsonResponse(200, record)

	case "/create":
		record := core.NewRecord(col)
		data, _ := req["data"].(map[string]interface{})
		for k, v := range data {
			record.Set(k, v)
		}
		if saveErr := h.app.Save(record); saveErr != nil {
			return h.errorResponse(400, saveErr.Error())
		}
		return h.jsonResponse(200, record)

	case "/update":
		id, _ := req["id"].(string)
		record, findErr := h.app.FindRecordById(col, id)
		if findErr != nil {
			return h.errorResponse(404, "record not found")
		}
		data, _ := req["data"].(map[string]interface{})
		for k, v := range data {
			record.Set(k, v)
		}
		if saveErr := h.app.Save(record); saveErr != nil {
			return h.errorResponse(400, saveErr.Error())
		}
		return h.jsonResponse(200, record)

	case "/delete":
		id, _ := req["id"].(string)
		record, findErr := h.app.FindRecordById(col, id)
		if findErr != nil {
			return h.errorResponse(404, "record not found")
		}
		if delErr := h.app.Delete(record); delErr != nil {
			return h.errorResponse(500, delErr.Error())
		}
		return h.jsonResponse(200, map[string]string{"deleted": id})

	default:
		return h.errorResponse(400, fmt.Sprintf("unknown records path: %s", path))
	}
}

// handleAuth handles auth operations via ZAP.
func (h *handler) handleAuth(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
	root := msg.Root()
	path := root.Text(fieldPath)
	body := root.Bytes(fieldBody)

	h.logger.Debug("zap: auth", "path", path, "from", from)

	var req map[string]interface{}
	if len(body) > 0 {
		json.Unmarshal(body, &req)
	}

	switch path {
	case "/identity":
		identity, _ := req["identity"].(string)
		password, _ := req["password"].(string)
		collection, _ := req["collection"].(string)
		if collection == "" {
			collection = core.CollectionNameSuperusers
		}

		col, err := h.app.FindCollectionByNameOrId(collection)
		if err != nil {
			return h.errorResponse(404, "auth collection not found")
		}

		record, err := h.app.FindAuthRecordByEmail(col, identity)
		if err != nil {
			return h.errorResponse(401, "invalid credentials")
		}

		if !record.ValidatePassword(password) {
			return h.errorResponse(401, "invalid credentials")
		}

		token, err := record.NewAuthToken()
		if err != nil {
			return h.errorResponse(500, "token generation failed")
		}

		return h.jsonResponse(200, map[string]interface{}{
			"token":  token,
			"record": record,
		})

	default:
		return h.errorResponse(400, fmt.Sprintf("unknown auth path: %s", path))
	}
}

// handleRealtime handles realtime subscription management via ZAP.
func (h *handler) handleRealtime(ctx context.Context, from string, msg *zaplib.Message) (*zaplib.Message, error) {
	root := msg.Root()
	path := root.Text(fieldPath)
	body := root.Bytes(fieldBody)

	h.logger.Debug("zap: realtime", "path", path, "from", from)

	var req map[string]interface{}
	if len(body) > 0 {
		json.Unmarshal(body, &req)
	}

	switch path {
	case "/subscribe":
		collections, _ := req["collections"].([]interface{})
		if len(collections) == 0 {
			return h.errorResponse(400, "collections array required")
		}

		h.subsMu.Lock()
		if h.subs[from] == nil {
			h.subs[from] = make(map[string]struct{})
		}
		subscribed := make([]string, 0, len(collections))
		for _, c := range collections {
			if name, ok := c.(string); ok && name != "" {
				h.subs[from][name] = struct{}{}
				subscribed = append(subscribed, name)
			}
		}
		h.subsMu.Unlock()

		h.logger.Info("zap: subscribed", "peer", from, "collections", subscribed)
		return h.jsonResponse(200, map[string]interface{}{
			"subscribed": subscribed,
		})

	case "/unsubscribe":
		collections, _ := req["collections"].([]interface{})

		h.subsMu.Lock()
		if len(collections) == 0 {
			// Unsubscribe from all
			delete(h.subs, from)
		} else {
			if peerSubs := h.subs[from]; peerSubs != nil {
				for _, c := range collections {
					if name, ok := c.(string); ok {
						delete(peerSubs, name)
					}
				}
				if len(peerSubs) == 0 {
					delete(h.subs, from)
				}
			}
		}
		h.subsMu.Unlock()

		return h.jsonResponse(200, map[string]string{"status": "unsubscribed"})

	case "/status":
		h.subsMu.RLock()
		peerSubs := h.subs[from]
		cols := make([]string, 0, len(peerSubs))
		for c := range peerSubs {
			cols = append(cols, c)
		}
		h.subsMu.RUnlock()

		return h.jsonResponse(200, map[string]interface{}{
			"subscriptions": cols,
		})

	default:
		return h.errorResponse(400, fmt.Sprintf("unknown realtime path: %s", path))
	}
}

// broadcastEvent pushes a record change event to all subscribed peers.
func (h *handler) broadcastEvent(collection string, action string, record *core.Record) {
	if h.node == nil {
		return
	}

	h.subsMu.RLock()
	defer h.subsMu.RUnlock()

	payload, _ := json.Marshal(map[string]interface{}{
		"action":     action,
		"collection": collection,
		"record":     record,
	})

	for peer, collections := range h.subs {
		if _, ok := collections[collection]; !ok {
			continue
		}

		msg, err := h.buildResponse(200, payload)
		if err != nil {
			h.logger.Warn("zap: failed to build broadcast", "error", err)
			continue
		}

		if err := h.node.Send(context.Background(), peer, msg); err != nil {
			h.logger.Debug("zap: send to peer failed, removing subscription",
				"peer", peer, "error", err)
			// Remove dead peer (defer unlock already held as RLock, so schedule cleanup)
			go func(p string) {
				h.subsMu.Lock()
				delete(h.subs, p)
				h.subsMu.Unlock()
			}(peer)
		}
	}
}

// jsonResponse builds a ZAP response message with JSON body.
func (h *handler) jsonResponse(status uint32, data interface{}) (*zaplib.Message, error) {
	body, _ := json.Marshal(data)
	return h.buildResponse(status, body)
}

// errorResponse builds a ZAP error response.
func (h *handler) errorResponse(status uint32, message string) (*zaplib.Message, error) {
	body, _ := json.Marshal(map[string]string{"error": message})
	return h.buildResponse(status, body)
}

// buildResponse constructs a ZAP response message.
func (h *handler) buildResponse(status uint32, body []byte) (*zaplib.Message, error) {
	b := zaplib.NewBuilder(len(body) + 128)
	obj := b.StartObject(20)
	obj.SetUint32(respStatus, status)
	obj.SetBytes(respBody, body)
	obj.FinishAsRoot()
	data := b.Finish()

	msg, err := zaplib.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("zap: build response: %w", err)
	}
	return msg, nil
}
