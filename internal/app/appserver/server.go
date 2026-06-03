package appserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/usewhale/whale/internal/app"
	"github.com/usewhale/whale/internal/app/service"
	"github.com/usewhale/whale/internal/runtime/protocol"
)

const maxClientMessageBytes = 16 * 1024 * 1024

func Run(ctx context.Context, cfg app.Config, start app.StartOptions, in io.Reader, out io.Writer) error {
	svc, err := service.New(ctx, cfg, start)
	if err != nil {
		return err
	}
	defer svc.Close()

	enc := json.NewEncoder(out)
	write := func(msg any) error {
		if err := enc.Encode(msg); err != nil {
			return fmt.Errorf("write app-server message: %w", err)
		}
		return nil
	}
	if err := writeRPCNotification(write, protocol.RPCMethodAppReady, appInfo(svc)); err != nil {
		return err
	}

	inputs := make(chan clientInput)
	go scanClientFrames(in, inputs)
	messages := svc.Messages()
	for {
		select {
		case msg := <-messages:
			if err := writeServiceMessageNotification(write, msg); err != nil {
				return err
			}
		case input := <-inputs:
			if input.done {
				return closeAfterClientDisconnect(svc, write)
			}
			if input.err != nil {
				if err := writeRPCError(write, rawNullID(), protocol.RPCErrorParseError, input.err.Error(), nil); err != nil {
					return err
				}
				continue
			}
			decoded, errResp := decodeClientFrame(input.raw)
			if errResp != nil {
				if err := writeRPCError(write, errResp.id, errResp.code, errResp.message, errResp.data); err != nil {
					return err
				}
				continue
			}
			if decoded.notification {
				continue
			}
			closeServer, err := handleRequest(svc, decoded.request, write)
			if err != nil {
				return err
			}
			if closeServer {
				return nil
			}
		case <-ctx.Done():
			if err := writeRPCNotification(write, protocol.RPCMethodAppClosed, protocol.ClosedNotification{Reason: "context_cancelled"}); err != nil {
				return err
			}
			return ctx.Err()
		}
	}
}

type serviceInfo interface {
	SessionID() string
	WorkspaceRoot() string
}

type rpcWriter func(any) error

func closeAfterClientDisconnect(svc *service.Service, write rpcWriter) error {
	svc.Dispatch(service.Intent{Kind: service.IntentShutdown})
	if err := writeRPCNotification(write, protocol.RPCMethodAppClosed, protocol.ClosedNotification{Reason: "eof"}); err != nil {
		return err
	}
	return nil
}

type clientInput struct {
	raw  []byte
	err  error
	done bool
}

func scanClientFrames(in io.Reader, out chan<- clientInput) {
	defer close(out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxClientMessageBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		out <- clientInput{raw: append([]byte(nil), line...)}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		out <- clientInput{err: fmt.Errorf("read client message: %w", err)}
	}
	out <- clientInput{done: true}
}

type decodedFrame struct {
	request      protocol.RPCRequest
	notification bool
}

type rpcDecodeError struct {
	id      json.RawMessage
	code    int
	message string
	data    json.RawMessage
}

func decodeClientFrame(raw []byte) (decodedFrame, *rpcDecodeError) {
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return decodedFrame{}, &rpcDecodeError{
			id:      rawNullID(),
			code:    protocol.RPCErrorParseError,
			message: fmt.Sprintf("parse error: %v", err),
		}
	}
	if envelope.JSONRPC != protocol.JSONRPCVersion || strings.TrimSpace(envelope.Method) == "" || len(envelope.Result) > 0 || len(envelope.Error) > 0 {
		return decodedFrame{}, &rpcDecodeError{
			id:      requestIDOrNull(envelope.ID),
			code:    protocol.RPCErrorInvalidRequest,
			message: "invalid JSON-RPC request",
		}
	}
	if len(envelope.ID) == 0 {
		return decodedFrame{notification: true}, nil
	}
	if !validRequestID(envelope.ID) {
		return decodedFrame{}, &rpcDecodeError{
			id:      rawNullID(),
			code:    protocol.RPCErrorInvalidRequest,
			message: "invalid JSON-RPC request id",
		}
	}
	return decodedFrame{
		request: protocol.RPCRequest{
			JSONRPC: envelope.JSONRPC,
			ID:      append(json.RawMessage(nil), envelope.ID...),
			Method:  envelope.Method,
			Params:  append(json.RawMessage(nil), envelope.Params...),
		},
	}, nil
}

func handleRequest(svc *service.Service, req protocol.RPCRequest, write rpcWriter) (bool, error) {
	switch req.Method {
	case protocol.RPCMethodAppInitialize:
		if err := writeRPCResult(write, req.ID, appInfo(svc)); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodThreadCurrent:
		thread, err := svc.CurrentThread()
		if err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInternal, err.Error(), nil)
		}
		if err := writeRPCResult(write, req.ID, protocol.ThreadCurrentResponse{Thread: thread}); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodThreadList:
		var params protocol.ThreadListParams
		if err := decodeOptionalParams(req.Params, &params); err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		threads, err := svc.ListThreads(params.Limit)
		if err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInternal, err.Error(), nil)
		}
		if err := writeRPCResult(write, req.ID, protocol.ThreadListResponse{Threads: threads}); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodThreadRead:
		var params protocol.ThreadReadParams
		if err := decodeParams(req.Params, &params); err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		thread, err := svc.ReadThread(params.ThreadID, params.IncludeTurns)
		if err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		if err := writeRPCResult(write, req.ID, protocol.ThreadReadResponse{Thread: thread}); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodThreadStart:
		thread, err := svc.StartThread()
		if err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		if err := writeRPCResult(write, req.ID, protocol.ThreadStartResponse{Thread: thread}); err != nil {
			return false, err
		}
		if err := writeRPCNotification(write, protocol.RPCMethodThreadStarted, protocol.ThreadStartedNotification{Thread: thread}); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodThreadResume:
		var params protocol.ThreadResumeParams
		if err := decodeParams(req.Params, &params); err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		thread, err := svc.ResumeThread(params.ThreadID)
		if err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		if err := writeRPCResult(write, req.ID, protocol.ThreadResumeResponse{Thread: thread}); err != nil {
			return false, err
		}
		if err := writeRPCNotification(write, protocol.RPCMethodThreadStatusChanged, protocol.ThreadStatusChangedNotification{ThreadID: thread.ID, Status: thread.Status}); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodTurnStart:
		var params protocol.TurnStartParams
		if err := decodeParams(req.Params, &params); err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		turn, err := svc.StartTurn(params)
		if err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		if err := writeRPCResult(write, req.ID, protocol.TurnStartResponse{Turn: turn}); err != nil {
			return false, err
		}
		if err := writeRPCNotification(write, protocol.RPCMethodThreadStatusChanged, protocol.ThreadStatusChangedNotification{ThreadID: turn.ThreadID, Status: protocol.ThreadStatusActive}); err != nil {
			return false, err
		}
		if err := writeRPCNotification(write, protocol.RPCMethodTurnStarted, protocol.TurnStartedNotification{ThreadID: turn.ThreadID, Turn: turn}); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodTurnInterrupt:
		var params protocol.TurnInterruptParams
		if err := decodeParams(req.Params, &params); err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		resp, err := svc.InterruptTurn(params)
		if err != nil {
			return false, writeRPCError(write, req.ID, protocol.RPCErrorInvalidParams, err.Error(), nil)
		}
		if err := writeRPCResult(write, req.ID, resp); err != nil {
			return false, err
		}
		return false, nil
	case protocol.RPCMethodAppShutdown:
		svc.Dispatch(service.Intent{Kind: service.IntentShutdown})
		if err := writeRPCResult(write, req.ID, protocol.AcceptedResponse{Accepted: true}); err != nil {
			return false, err
		}
		if err := writeRPCNotification(write, protocol.RPCMethodAppClosed, protocol.ClosedNotification{Reason: "shutdown"}); err != nil {
			return false, err
		}
		return true, nil
	default:
		if err := writeRPCError(write, req.ID, protocol.RPCErrorMethodNotFound, "method not found: "+req.Method, nil); err != nil {
			return false, err
		}
		return false, nil
	}
}

func decodeParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return errors.New("params are required")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func decodeOptionalParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return nil
	}
	return decodeParams(raw, out)
}

func appInfo(info serviceInfo) protocol.AppInfo {
	return protocol.AppInfo{
		ProtocolVersion: "2",
		SessionID:       info.SessionID(),
		WorkspaceRoot:   info.WorkspaceRoot(),
		Capabilities: []string{
			protocol.RPCMethodAppInitialize,
			protocol.RPCMethodAppShutdown,
			protocol.RPCMethodAppReady,
			protocol.RPCMethodAppClosed,
			protocol.RPCMethodThreadCurrent,
			protocol.RPCMethodThreadList,
			protocol.RPCMethodThreadRead,
			protocol.RPCMethodThreadStart,
			protocol.RPCMethodThreadResume,
			protocol.RPCMethodThreadStarted,
			protocol.RPCMethodThreadStatusChanged,
			protocol.RPCMethodTurnStart,
			protocol.RPCMethodTurnInterrupt,
			protocol.RPCMethodTurnStarted,
			protocol.RPCMethodTurnCompleted,
			protocol.RPCMethodItemStarted,
			protocol.RPCMethodItemDelta,
			protocol.RPCMethodItemCompleted,
		},
	}
}

func writeServiceEventNotifications(svc *service.Service, write rpcWriter, ev service.Event) error {
	threadID := svc.SessionID()
	active, _ := svc.ActiveTurnSnapshot()
	for _, msg := range service.EventServiceMessages(threadID, ev, active) {
		if err := writeServiceMessageNotification(write, msg); err != nil {
			return err
		}
	}
	return nil
}

func writeServiceMessageNotification(write rpcWriter, msg protocol.ServiceMessage) error {
	switch msg.Type {
	case protocol.ServiceMessageTurnCompleted:
		if msg.Turn == nil {
			return nil
		}
		return writeRPCNotification(write, protocol.RPCMethodTurnCompleted, protocol.TurnCompletedNotification{ThreadID: msg.ThreadID, Turn: *msg.Turn})
	case protocol.ServiceMessageThreadStatusChanged:
		return writeRPCNotification(write, protocol.RPCMethodThreadStatusChanged, protocol.ThreadStatusChangedNotification{ThreadID: msg.ThreadID, Status: msg.ThreadStatus})
	case protocol.ServiceMessageItemStarted:
		if msg.Item == nil || strings.TrimSpace(msg.Item.ID) == "" {
			return nil
		}
		return writeRPCNotification(write, protocol.RPCMethodItemStarted, protocol.ItemStartedNotification{
			ThreadID:    msg.ThreadID,
			TurnID:      msg.TurnID,
			Item:        *msg.Item,
			StartedAtMS: unixMillis(firstNonZeroTime(msg.StartedAt, msg.Item.CreatedAt)),
		})
	case protocol.ServiceMessageItemDelta:
		if msg.Delta == nil || strings.TrimSpace(msg.Delta.ItemID) == "" {
			return nil
		}
		return writeRPCNotification(write, protocol.RPCMethodItemDelta, protocol.ItemDeltaNotification{
			ThreadID: msg.ThreadID,
			TurnID:   msg.TurnID,
			ItemID:   msg.Delta.ItemID,
			Delta:    *msg.Delta,
		})
	case protocol.ServiceMessageItemCompleted:
		if msg.Item == nil || strings.TrimSpace(msg.Item.ID) == "" {
			return nil
		}
		return writeRPCNotification(write, protocol.RPCMethodItemCompleted, protocol.ItemCompletedNotification{
			ThreadID:      msg.ThreadID,
			TurnID:        msg.TurnID,
			Item:          *msg.Item,
			CompletedAtMS: unixMillis(firstNonZeroTime(msg.CompletedAt, msg.Item.UpdatedAt, msg.Item.CreatedAt)),
		})
	default:
		return nil
	}
}

func unixMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano() / int64(time.Millisecond)
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func writeRPCNotification(write rpcWriter, method string, params any) error {
	rawParams, err := marshalParams(params)
	if err != nil {
		return err
	}
	return write(protocol.RPCNotification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  rawParams,
	})
}

func writeRPCResult(write rpcWriter, id json.RawMessage, result any) error {
	rawResult, err := marshalParams(result)
	if err != nil {
		return err
	}
	return write(protocol.RPCResponse{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      append(json.RawMessage(nil), id...),
		Result:  rawResult,
	})
}

func writeRPCError(write rpcWriter, id json.RawMessage, code int, message string, data any) error {
	rawData, err := marshalOptional(data)
	if err != nil {
		return err
	}
	return write(protocol.RPCResponse{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      requestIDOrNull(id),
		Error: &protocol.RPCErrorObject{
			Code:    code,
			Message: message,
			Data:    rawData,
		},
	})
}

func marshalParams(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func marshalOptional(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	return marshalParams(value)
}

func validRequestID(id json.RawMessage) bool {
	var value any
	if err := json.Unmarshal(id, &value); err != nil {
		return false
	}
	switch value.(type) {
	case string, float64:
		return true
	default:
		return false
	}
}

func requestIDOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 || !validRequestID(id) {
		return rawNullID()
	}
	return append(json.RawMessage(nil), id...)
}

func rawNullID() json.RawMessage {
	return json.RawMessage("null")
}
