package ubot

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Laky-64/gologging"
	tg "github.com/amarnathcjd/gogram/telegram"

	"main/ntgcalls"
)

type Context struct {
	binding *ntgcalls.Client
	app     *tg.Client
	self    *tg.UserObj

	mutedByAdminMutex sync.RWMutex
	mutedByAdmin      []int64

	presentationsMutex sync.RWMutex
	presentations      []int64

	pendingPresentationMutex sync.RWMutex
	pendingPresentation      map[int64]bool

	p2pConfigsMutex sync.RWMutex
	p2pConfigs      map[int64]*P2PConfig

	inputCallsMutex sync.RWMutex
	inputCalls      map[int64]*tg.InputPhoneCall

	inputGroupCallsMutex sync.RWMutex
	inputGroupCalls      map[int64]tg.InputGroupCall

	participantsMutex sync.Mutex
	callParticipants  map[int64]*CallParticipantsCache

	pendingConnectionsMutex sync.RWMutex
	pendingConnections      map[int64]*PendingConnection

	callSourcesMutex sync.RWMutex
	callSources      map[int64]*CallSources

	waitConnectMutex sync.RWMutex
	waitConnect      map[int64]chan error

	callbacksMutex        sync.RWMutex
	incomingCallCallbacks []func(client *Context, chatId int64)
	streamEndCallbacks    []ntgcalls.StreamEndCallback
	frameCallbacks        []ntgcalls.FrameCallback

	groupCallMessageCallbacks []func(*GroupCallMessageEvent)
}

var (
	ErrAlreadyInGroupCall     = errors.New("already in group call")
	ErrGroupCallAlreadyClosed = errors.New("group call already closed")
)

func (ctx *Context) OnGroupCallMessage(callback func(*GroupCallMessageEvent)) {
	ctx.callbacksMutex.Lock()
	defer ctx.callbacksMutex.Unlock()

	ctx.groupCallMessageCallbacks = append(ctx.groupCallMessageCallbacks, callback)
}

func NewContext(app *tg.Client) *Context {
	client := &Context{
		binding: ntgcalls.NTgCalls(),
		app:     app,

		pendingPresentation: make(map[int64]bool),
		p2pConfigs:          make(map[int64]*P2PConfig),
		inputCalls:          make(map[int64]*tg.InputPhoneCall),
		inputGroupCalls:     make(map[int64]tg.InputGroupCall),
		pendingConnections:  make(map[int64]*PendingConnection),
		callParticipants:    make(map[int64]*CallParticipantsCache),
		callSources:         make(map[int64]*CallSources),
		waitConnect:         make(map[int64]chan error),
	}
	if app.IsConnected() {
		me := app.Me()

		if me.ID == 0 {
			var err error
			me, err = app.GetMe()
			if err != nil {
				gologging.Fatal(err)
			}
		}

		client.self = me
	}

	client.handleUpdates()
	return client
}

func (ctx *Context) Calls() map[int64]*ntgcalls.CallInfo {
	return ctx.binding.Calls()
}

func (ctx *Context) Mute(chatID int64) (bool, error) {
	return ctx.binding.Mute(chatID)
}

func (ctx *Context) Pause(chatID int64) (bool, error) {
	return ctx.binding.Pause(chatID)
}

func (ctx *Context) Resume(chatID int64) (bool, error) {
	return ctx.binding.Resume(chatID)
}

func (ctx *Context) Unmute(chatID int64) (bool, error) {
	return ctx.binding.Unmute(chatID)
}

func (ctx *Context) Play(
	chatID int64,
	mediaDescription ntgcalls.MediaDescription,
) error {
	if ctx.binding.Calls()[chatID] != nil {
		return ctx.binding.SetStreamSources(
			chatID,
			ntgcalls.CaptureStream,
			mediaDescription,
		)
	}

	err := ctx.connectCall(chatID, mediaDescription, "")
	if err != nil {
		return err
	}

	if chatID < 0 {
		err = ctx.joinPresentation(chatID, mediaDescription.Screen != nil)
		if err != nil {
			return err
		}
		return ctx.updateSources(chatID)
	}

	return nil
}

func (ctx *Context) Record(
	chatID int64,
	mediaDescription ntgcalls.MediaDescription,
) error {
	if ctx.binding.Calls()[chatID] != nil {
		return ctx.binding.SetStreamSources(
			chatID,
			ntgcalls.PlaybackStream,
			mediaDescription,
		)
	}

	return ctx.Play(chatID, ntgcalls.MediaDescription{})
}

func (ctx *Context) Stop(chatID int64) error {
	// Clean up presentations
	ctx.presentationsMutex.Lock()
	ctx.presentations = stdRemove(ctx.presentations, chatID)
	ctx.presentationsMutex.Unlock()

	ctx.callSourcesMutex.Lock()
	delete(ctx.callSources, chatID)
	ctx.callSourcesMutex.Unlock()

	ctx.waitConnectMutex.Lock()
	if waitChan, exists := ctx.waitConnect[chatID]; exists {
		select {
		case waitChan <- fmt.Errorf("call stopped"):
		default:
		}
		delete(ctx.waitConnect, chatID)
	}
	ctx.waitConnectMutex.Unlock()

	err := ctx.binding.Stop(chatID)
	if err != nil {
		return err
	}

	ctx.inputGroupCallsMutex.RLock()
	inputGroupCall, ok := ctx.inputGroupCalls[chatID]
	ctx.inputGroupCallsMutex.RUnlock()

	if !ok {
		return nil
	}

	_, err = ctx.app.PhoneLeaveGroupCall(inputGroupCall, 0)
	if err != nil {
		return err
	}
	return nil
}

func (ctx *Context) OnIncomingCall(
	callback func(client *Context, chatId int64),
) {
	ctx.callbacksMutex.Lock()
	defer ctx.callbacksMutex.Unlock()
	ctx.incomingCallCallbacks = append(ctx.incomingCallCallbacks, callback)
}

func (ctx *Context) OnStreamEnd(callback ntgcalls.StreamEndCallback) {
	ctx.callbacksMutex.Lock()
	defer ctx.callbacksMutex.Unlock()
	ctx.streamEndCallbacks = append(ctx.streamEndCallbacks, callback)
}

func (ctx *Context) OnFrame(callback ntgcalls.FrameCallback) {
	ctx.callbacksMutex.Lock()
	defer ctx.callbacksMutex.Unlock()
	ctx.frameCallbacks = append(ctx.frameCallbacks, callback)
}

func (ctx *Context) Close() {
	if ctx.binding == nil {
		return
	}

	for chatId := range ctx.binding.Calls() {
		ctx.binding.Stop(chatId)
	}

	ctx.p2pConfigsMutex.Lock()
	ctx.p2pConfigs = nil
	ctx.p2pConfigsMutex.Unlock()

	ctx.inputCallsMutex.Lock()
	ctx.inputCalls = nil
	ctx.inputCallsMutex.Unlock()

	ctx.inputGroupCallsMutex.Lock()
	ctx.inputGroupCalls = nil
	ctx.inputGroupCallsMutex.Unlock()

	ctx.pendingConnectionsMutex.Lock()
	ctx.pendingConnections = nil
	ctx.pendingConnectionsMutex.Unlock()

	ctx.participantsMutex.Lock()
	ctx.callParticipants = nil
	ctx.participantsMutex.Unlock()

	ctx.callSourcesMutex.Lock()
	ctx.callSources = nil
	ctx.callSourcesMutex.Unlock()

	ctx.waitConnectMutex.Lock()
	ctx.waitConnect = nil
	ctx.waitConnectMutex.Unlock()

	ctx.binding.Free()
	ctx.binding = nil
}

//------------------ work in new gogram version ---------------

// func (ctx *Context) IsInGroupCall(chatId int64) bool {
// 	ctx.inputGroupCallsMutex.RLock()
// 	defer ctx.inputGroupCallsMutex.RUnlock()

// 	_, ok := ctx.inputGroupCalls[chatId]
// 	return ok
// }

// func (ctx *Context) StartGroupCall(chatId int64) error {
// 	if ctx.self == nil {
// 		return fmt.Errorf("assistant identity is not loaded")
// 	}

// 	if _, err := ctx.GetInputGroupCall(chatId); err == nil {
// 		return ctx.JoinGroupCall(chatId)
// 	}

// 	call, err := ctx.app.StartGroupCall(chatId)
// 	if err != nil {
// 		return err
// 	}

// 	ctx.inputGroupCallsMutex.Lock()
// 	ctx.inputGroupCalls[chatId] = call
// 	ctx.inputGroupCallsMutex.Unlock()

// 	return nil
// }

// func (ctx *Context) JoinGroupCall(chatId int64) error {
// 	if ctx.self == nil {
// 		return fmt.Errorf("assistant identity is not loaded")
// 	}

// 	inputGroupCall, err := ctx.GetInputGroupCall(chatId)
// 	if err != nil {
// 		return err
// 	}
// 	if inputGroupCall == nil {
// 		return fmt.Errorf("group call for chatId %d is closed", chatId)
// 	}

// 	_, err = ctx.app.PhoneJoinGroupCall(&tg.PhoneJoinGroupCallParams{
// 		Muted:        false,
// 		VideoStopped: true,
// 		Call:         inputGroupCall,
// 		Params: &tg.DataJson{
// 			Data: `{"transport": null}`,
// 		},
// 		JoinAs: &tg.InputPeerUser{
// 			UserID:     ctx.self.ID,
// 			AccessHash: ctx.self.AccessHash,
// 		},
// 	})
// 	if err != nil {
// 		return err
// 	}

// 	ctx.inputGroupCallsMutex.Lock()
// 	ctx.inputGroupCalls[chatId] = inputGroupCall
// 	ctx.inputGroupCallsMutex.Unlock()

// 	return nil
// }

// func (ctx *Context) EndGroupCall(chatId int64) error {
// 	ctx.inputGroupCallsMutex.RLock()
// 	inputGroupCall, ok := ctx.inputGroupCalls[chatId]
// 	ctx.inputGroupCallsMutex.RUnlock()

// 	if !ok {
// 		var err error
// 		inputGroupCall, err = ctx.GetInputGroupCall(chatId)
// 		if err != nil {
// 			return err
// 		}
// 	}
// 	if inputGroupCall == nil {
// 		return fmt.Errorf("group call for chatId %d is closed", chatId)
// 	}

// 	if err := ctx.app.DiscardGroupCall(inputGroupCall); err != nil {
// 		return err
// 	}

// 	ctx.inputGroupCallsMutex.Lock()
// 	delete(ctx.inputGroupCalls, chatId)
// 	ctx.inputGroupCallsMutex.Unlock()

// 	return nil
// }

// func (ctx *Context) ExportGroupCallInvite(chatId int64, canSelfUnmute bool) (string, error) {
// 	inputGroupCall, err := ctx.GetInputGroupCall(chatId)
// 	if err != nil {
// 		return "", err
// 	}
// 	if inputGroupCall == nil {
// 		return "", fmt.Errorf("group call for chatId %d is closed", chatId)
// 	}

// 	return ctx.app.ExportGroupCallInvite(inputGroupCall, canSelfUnmute)
// }
//------------------ work in new gogram version ---------------

func (ctx *Context) clearInputGroupCall(chatId int64) {
	ctx.inputGroupCallsMutex.Lock()
	delete(ctx.inputGroupCalls, chatId)
	ctx.inputGroupCallsMutex.Unlock()
}

func isClosedGroupCallErr(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "closed") ||
		strings.Contains(msg, "group call for chatid") && strings.Contains(msg, "closed")
}

func isAlreadyInGroupCallErr(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "user_already_participant") ||
		strings.Contains(msg, "already_participant") ||
		(strings.Contains(msg, "participant") && strings.Contains(msg, "already")) ||
		strings.Contains(msg, "ssrc_duplicate")
}

func (ctx *Context) extractInputGroupCall(updates tg.Updates) (tg.InputGroupCall, bool) {
	switch upd := updates.(type) {
	case *tg.UpdatesObj:
		return extractInputGroupCallFromUpdates(upd.Updates)

	case *tg.UpdateShort:
		return extractInputGroupCallFromUpdates([]tg.Update{upd.Update})

	default:
		return nil, false
	}
}

func extractInputGroupCallFromUpdates(updates []tg.Update) (tg.InputGroupCall, bool) {
	for _, update := range updates {
		groupCallUpdate, ok := update.(*tg.UpdateGroupCall)
		if !ok || groupCallUpdate.Call == nil {
			continue
		}

		switch call := groupCallUpdate.Call.(type) {
		case *tg.GroupCallObj:
			return &tg.InputGroupCallObj{
				ID:         call.ID,
				AccessHash: call.AccessHash,
			}, true

		case tg.InputGroupCall:
			return call, true
		}
	}

	return nil, false
}

func (ctx *Context) IsInGroupCall(chatId int64) bool {
	ctx.inputGroupCallsMutex.RLock()
	defer ctx.inputGroupCallsMutex.RUnlock()

	_, ok := ctx.inputGroupCalls[chatId]
	return ok
}

func (ctx *Context) StartGroupCall(chatId int64) error {
	if ctx.self == nil {
		return fmt.Errorf("assistant identity is not loaded")
	}

	if _, err := ctx.GetInputGroupCall(chatId); err == nil {
		return ctx.JoinGroupCall(chatId)
	}

	ctx.clearInputGroupCall(chatId)

	peer, err := ctx.app.ResolvePeer(chatId)
	if err != nil {
		return err
	}

	updates, err := ctx.app.PhoneCreateGroupCall(&tg.PhoneCreateGroupCallParams{
		Peer:     peer,
		RandomID: int32(tg.GenRandInt()),
	})
	if err != nil {
		if tg.MatchError(err, "GROUPCALL_EXISTS") ||
			strings.Contains(strings.ToLower(err.Error()), "groupcall_exists") {
			return ctx.JoinGroupCall(chatId)
		}

		return err
	}

	call, ok := ctx.extractInputGroupCall(updates)
	if !ok || call == nil {
		return fmt.Errorf("group call started but no call info returned")
	}

	ctx.inputGroupCallsMutex.Lock()
	ctx.inputGroupCalls[chatId] = call
	ctx.inputGroupCallsMutex.Unlock()

	return nil
}

func (ctx *Context) JoinGroupCall(chatId int64) error {
	if ctx.self == nil {
		return fmt.Errorf("assistant identity is not loaded")
	}

	inputGroupCall, err := ctx.GetInputGroupCall(chatId)
	if err != nil {
		return err
	}
	if inputGroupCall == nil {
		return fmt.Errorf("group call for chatId %d is closed", chatId)
	}

	_, err = ctx.app.PhoneJoinGroupCall(&tg.PhoneJoinGroupCallParams{
		Muted:        false,
		VideoStopped: true,
		Call:         inputGroupCall,
		Params: &tg.DataJson{
			Data: `{"transport": null}`,
		},
		JoinAs: &tg.InputPeerUser{
			UserID:     ctx.self.ID,
			AccessHash: ctx.self.AccessHash,
		},
	})
	if err != nil {
		if isAlreadyInGroupCallErr(err) {
			return ErrAlreadyInGroupCall
		}

		return err
	}

	ctx.inputGroupCallsMutex.Lock()
	ctx.inputGroupCalls[chatId] = inputGroupCall
	ctx.inputGroupCallsMutex.Unlock()

	return nil
}

func (ctx *Context) EndGroupCall(chatId int64) error {
	ctx.inputGroupCallsMutex.RLock()
	inputGroupCall, ok := ctx.inputGroupCalls[chatId]
	ctx.inputGroupCallsMutex.RUnlock()

	if !ok {
		var err error
		inputGroupCall, err = ctx.GetInputGroupCall(chatId)
		if err != nil {
			if isClosedGroupCallErr(err) {
				ctx.clearInputGroupCall(chatId)
				return ErrGroupCallAlreadyClosed
			}

			return err
		}
	}
	if inputGroupCall == nil {
		ctx.clearInputGroupCall(chatId)
		return ErrGroupCallAlreadyClosed
	}

	if _, err := ctx.app.PhoneDiscardGroupCall(inputGroupCall); err != nil {
		if isClosedGroupCallErr(err) {
			ctx.clearInputGroupCall(chatId)
			return ErrGroupCallAlreadyClosed
		}

		return err
	}

	ctx.clearInputGroupCall(chatId)
	return nil
}

func (ctx *Context) ExportGroupCallInvite(chatId int64, canSelfUnmute bool) (string, error) {
	inputGroupCall, err := ctx.GetInputGroupCall(chatId)
	if err != nil {
		return "", err
	}
	if inputGroupCall == nil {
		return "", fmt.Errorf("group call for chatId %d is closed", chatId)
	}

	invite, err := ctx.app.PhoneExportGroupCallInvite(canSelfUnmute, inputGroupCall)
	if err != nil {
		return "", err
	}
	if invite == nil || invite.Link == "" {
		return "", fmt.Errorf("empty group call invite")
	}

	return invite.Link, nil
}
