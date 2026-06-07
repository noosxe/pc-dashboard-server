package connectrpc

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	pcdv1 "github.com/noosxe/pc-dashboard-server/pkg/api/pcd/v1"
	"github.com/noosxe/pc-dashboard-server/pkg/api/pcd/v1/pcdv1connect"
	"github.com/noosxe/pc-dashboard-server/pkg/dpms"
	"github.com/noosxe/pc-dashboard-server/pkg/lock"
	"github.com/noosxe/pc-dashboard-server/pkg/metrics"
	"github.com/noosxe/pc-dashboard-server/pkg/mpris"
	"github.com/noosxe/pc-dashboard-server/pkg/notifications"
	"github.com/noosxe/pc-dashboard-server/pkg/power"
	"google.golang.org/protobuf/types/known/structpb"
)

// Server implements all generated ConnectRPC services and multiplexes stream subscriptions.
type Server struct {
	logger *slog.Logger

	// Telemetry streams
	telemetryMu          sync.RWMutex
	telemetrySubscribers map[chan *pcdv1.TelemetryPayload]bool

	// Notification streams
	notificationMu          sync.RWMutex
	notificationSubscribers map[chan *pcdv1.NotificationEvent]bool

	// Media player streams
	mediaMu          sync.RWMutex
	mediaSubscribers map[chan *pcdv1.MediaStatePayload]bool

	// System state streams (Lock, DPMS, Power Profiles)
	systemMu          sync.RWMutex
	systemSubscribers map[chan *pcdv1.StreamSystemStateResponse]bool

	// Bluetooth state streams
	bluetoothMu          sync.RWMutex
	bluetoothSubscribers map[chan *pcdv1.BluetoothStatePayload]bool

	// Callbacks wired to the daemon engine
	onConfigChange         func(intervalMs int)
	onAction               func(command string)
	onNotificationCommand  func(req notifications.NotificationRequest) (uint32, error)
	onMediaCommand         func(playerName string, command string, args map[string]interface{}) error
	onPowerProfileCommand  func(profile string) error
	getInitialSystemStates func() []*pcdv1.StreamSystemStateResponse
}

// NewServer initializes a thread-safe ConnectRPC server wrapper.
func NewServer(
	logger *slog.Logger,
	onConfigChange func(intervalMs int),
	onAction func(command string),
	onNotificationCommand func(req notifications.NotificationRequest) (uint32, error),
	onMediaCommand func(playerName string, command string, args map[string]interface{}) error,
	onPowerProfileCommand func(profile string) error,
	getInitialSystemStates func() []*pcdv1.StreamSystemStateResponse,
) *Server {
	return &Server{
		logger:                  logger,
		telemetrySubscribers:    make(map[chan *pcdv1.TelemetryPayload]bool),
		notificationSubscribers: make(map[chan *pcdv1.NotificationEvent]bool),
		mediaSubscribers:        make(map[chan *pcdv1.MediaStatePayload]bool),
		systemSubscribers:       make(map[chan *pcdv1.StreamSystemStateResponse]bool),
		bluetoothSubscribers:    make(map[chan *pcdv1.BluetoothStatePayload]bool),
		onConfigChange:          onConfigChange,
		onAction:                onAction,
		onNotificationCommand:   onNotificationCommand,
		onMediaCommand:          onMediaCommand,
		onPowerProfileCommand:   onPowerProfileCommand,
		getInitialSystemStates:  getInitialSystemStates,
	}
}

// RegisterHandlers mounts all ConnectRPC service handlers onto the provided HTTP mux.
func (s *Server) RegisterHandlers(mux *http.ServeMux) {
	mux.Handle(pcdv1connect.NewTelemetryServiceHandler(s))
	mux.Handle(pcdv1connect.NewNotificationServiceHandler(s))
	mux.Handle(pcdv1connect.NewMediaServiceHandler(s))
	mux.Handle(pcdv1connect.NewSystemServiceHandler(s))
	mux.Handle(pcdv1connect.NewBluetoothServiceHandler(s))
}

// ==========================================
// Telemetry Service Implementation
// ==========================================

func (s *Server) StreamTelemetry(
	ctx context.Context,
	req *connect.Request[pcdv1.StreamTelemetryRequest],
	stream *connect.ServerStream[pcdv1.StreamTelemetryResponse],
) error {
	if req.Msg.IntervalMs > 0 && s.onConfigChange != nil {
		s.onConfigChange(int(req.Msg.IntervalMs))
	}

	ch := make(chan *pcdv1.TelemetryPayload, 10)
	s.telemetryMu.Lock()
	s.telemetrySubscribers[ch] = true
	s.telemetryMu.Unlock()

	defer func() {
		s.telemetryMu.Lock()
		delete(s.telemetrySubscribers, ch)
		close(ch)
		s.telemetryMu.Unlock()
	}()

	s.logger.Debug("ConnectRPC Telemetry subscriber connected")

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("ConnectRPC Telemetry subscriber disconnected")
			return nil
		case payload := <-ch:
			if err := stream.Send(&pcdv1.StreamTelemetryResponse{Payload: payload}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) BroadcastTelemetry(sysMetrics metrics.SystemMetrics) {
	payload := mapTelemetryPayload(sysMetrics)
	s.telemetryMu.RLock()
	defer s.telemetryMu.RUnlock()

	for ch := range s.telemetrySubscribers {
		select {
		case ch <- payload:
		default:
			// Non-blocking write: drop if subscriber buffer is full
		}
	}
}

// ==========================================
// Notification Service Implementation
// ==========================================

func (s *Server) StreamNotifications(
	ctx context.Context,
	req *connect.Request[pcdv1.StreamNotificationsRequest],
	stream *connect.ServerStream[pcdv1.StreamNotificationsResponse],
) error {
	ch := make(chan *pcdv1.NotificationEvent, 10)
	s.notificationMu.Lock()
	s.notificationSubscribers[ch] = true
	s.notificationMu.Unlock()

	defer func() {
		s.notificationMu.Lock()
		delete(s.notificationSubscribers, ch)
		close(ch)
		s.notificationMu.Unlock()
	}()

	s.logger.Debug("ConnectRPC Notification subscriber connected")

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("ConnectRPC Notification subscriber disconnected")
			return nil
		case ev := <-ch:
			if err := stream.Send(&pcdv1.StreamNotificationsResponse{
				Timestamp: time.Now().Unix(),
				Event:     ev,
			}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) SendNotification(
	ctx context.Context,
	req *connect.Request[pcdv1.SendNotificationRequest],
) (*connect.Response[pcdv1.SendNotificationResponse], error) {
	if req.Msg.Notification == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("notification parameter is missing"))
	}
	if s.onNotificationCommand == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("notification command handler not registered"))
	}

	hints := make(map[string]interface{})
	if req.Msg.Notification.Hints != nil {
		hints = req.Msg.Notification.Hints.AsMap()
	}

	nr := notifications.NotificationRequest{
		AppName:       req.Msg.Notification.AppName,
		ReplacesID:    req.Msg.Notification.ReplacesId,
		AppIcon:       req.Msg.Notification.AppIcon,
		Summary:       req.Msg.Notification.Summary,
		Body:          req.Msg.Notification.Body,
		Actions:       req.Msg.Notification.Actions,
		Hints:         hints,
		ExpireTimeout: req.Msg.Notification.ExpireTimeout,
	}

	id, err := s.onNotificationCommand(nr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pcdv1.SendNotificationResponse{Id: id}), nil
}

func (s *Server) TriggerNotificationAction(
	ctx context.Context,
	req *connect.Request[pcdv1.TriggerNotificationActionRequest],
) (*connect.Response[pcdv1.TriggerNotificationActionResponse], error) {
	// Custom action dispatching (delegated to system listener, not currently implemented in engine loop)
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("notification action triggers not yet supported"))
}

func (s *Server) DismissNotification(
	ctx context.Context,
	req *connect.Request[pcdv1.DismissNotificationRequest],
) (*connect.Response[pcdv1.DismissNotificationResponse], error) {
	// Client notification dismissal is currently stubbed/unsupported by direct daemon
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("notification dismiss not yet supported"))
}

func (s *Server) BroadcastNotification(ev notifications.NotificationEvent) {
	payload := mapNotificationEvent(ev)
	s.notificationMu.RLock()
	defer s.notificationMu.RUnlock()

	for ch := range s.notificationSubscribers {
		select {
		case ch <- payload:
		default:
		}
	}
}

// ==========================================
// Media Service Implementation
// ==========================================

func (s *Server) StreamMediaState(
	ctx context.Context,
	req *connect.Request[pcdv1.StreamMediaStateRequest],
	stream *connect.ServerStream[pcdv1.StreamMediaStateResponse],
) error {
	ch := make(chan *pcdv1.MediaStatePayload, 10)
	s.mediaMu.Lock()
	s.mediaSubscribers[ch] = true
	s.mediaMu.Unlock()

	defer func() {
		s.mediaMu.Lock()
		delete(s.mediaSubscribers, ch)
		close(ch)
		s.mediaMu.Unlock()
	}()

	s.logger.Debug("ConnectRPC Media subscriber connected")

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("ConnectRPC Media subscriber disconnected")
			return nil
		case payload := <-ch:
			if err := stream.Send(&pcdv1.StreamMediaStateResponse{
				Timestamp: time.Now().Unix(),
				Payload:   payload,
			}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) SendMediaCommand(
	ctx context.Context,
	req *connect.Request[pcdv1.SendMediaCommandRequest],
) (*connect.Response[pcdv1.SendMediaCommandResponse], error) {
	if s.onMediaCommand == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("media command handler not registered"))
	}

	args := make(map[string]interface{})
	if req.Msg.OffsetMicroseconds != nil {
		args["offset_microseconds"] = *req.Msg.OffsetMicroseconds
	}
	if req.Msg.PositionMicroseconds != nil {
		args["position_microseconds"] = *req.Msg.PositionMicroseconds
	}
	if req.Msg.TrackId != nil {
		args["track_id"] = *req.Msg.TrackId
	}
	if req.Msg.Volume != nil {
		args["volume"] = *req.Msg.Volume
	}

	err := s.onMediaCommand(req.Msg.PlayerName, req.Msg.Command, args)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pcdv1.SendMediaCommandResponse{Success: true}), nil
}

func (s *Server) BroadcastMediaState(ev mpris.MediaEvent) {
	payload := mapMediaEvent(ev)
	s.mediaMu.RLock()
	defer s.mediaMu.RUnlock()

	for ch := range s.mediaSubscribers {
		select {
		case ch <- payload:
		default:
		}
	}
}

// ==========================================
// System Service Implementation
// ==========================================

func (s *Server) StreamSystemState(
	ctx context.Context,
	req *connect.Request[pcdv1.StreamSystemStateRequest],
	stream *connect.ServerStream[pcdv1.StreamSystemStateResponse],
) error {
	ch := make(chan *pcdv1.StreamSystemStateResponse, 10)
	s.systemMu.Lock()
	s.systemSubscribers[ch] = true
	s.systemMu.Unlock()

	defer func() {
		s.systemMu.Lock()
		delete(s.systemSubscribers, ch)
		close(ch)
		s.systemMu.Unlock()
	}()

	s.logger.Debug("ConnectRPC System subscriber connected")

	// Emit initial cached states
	if s.getInitialSystemStates != nil {
		initialStates := s.getInitialSystemStates()
		for _, state := range initialStates {
			if err := stream.Send(state); err != nil {
				return err
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("ConnectRPC System subscriber disconnected")
			return nil
		case resp := <-ch:
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}

func (s *Server) ExecuteSystemAction(
	ctx context.Context,
	req *connect.Request[pcdv1.ExecuteSystemActionRequest],
) (*connect.Response[pcdv1.ExecuteSystemActionResponse], error) {
	if s.onAction == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("system action handler not registered"))
	}

	s.onAction(req.Msg.Command)
	return connect.NewResponse(&pcdv1.ExecuteSystemActionResponse{Success: true}), nil
}

func (s *Server) SetPowerProfile(
	ctx context.Context,
	req *connect.Request[pcdv1.SetPowerProfileRequest],
) (*connect.Response[pcdv1.SetPowerProfileResponse], error) {
	if s.onPowerProfileCommand == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("power profile command handler not registered"))
	}

	err := s.onPowerProfileCommand(req.Msg.Profile)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pcdv1.SetPowerProfileResponse{Success: true}), nil
}

func (s *Server) BroadcastSystemLock(ev lock.SessionLockEvent) {
	s.systemMu.RLock()
	defer s.systemMu.RUnlock()

	resp := &pcdv1.StreamSystemStateResponse{
		Timestamp: time.Now().Unix(),
		Event: &pcdv1.StreamSystemStateResponse_SessionLock{
			SessionLock: &pcdv1.SessionLockEvent{Locked: ev.Locked},
		},
	}

	for ch := range s.systemSubscribers {
		select {
		case ch <- resp:
		default:
		}
	}
}

func (s *Server) BroadcastDpms(ev dpms.DpmsEvent) {
	s.systemMu.RLock()
	defer s.systemMu.RUnlock()

	resp := &pcdv1.StreamSystemStateResponse{
		Timestamp: time.Now().Unix(),
		Event: &pcdv1.StreamSystemStateResponse_DpmsState{
			DpmsState: &pcdv1.DpmsEvent{State: ev.State},
		},
	}

	for ch := range s.systemSubscribers {
		select {
		case ch <- resp:
		default:
		}
	}
}

func (s *Server) BroadcastPowerProfile(ev power.PowerProfileState) {
	s.systemMu.RLock()
	defer s.systemMu.RUnlock()

	profiles := make([]*pcdv1.PowerProfile, len(ev.AvailableProfiles))
	for i, p := range ev.AvailableProfiles {
		profiles[i] = &pcdv1.PowerProfile{Profile: p.Profile}
	}

	resp := &pcdv1.StreamSystemStateResponse{
		Timestamp: time.Now().Unix(),
		Event: &pcdv1.StreamSystemStateResponse_PowerProfileState{
			PowerProfileState: &pcdv1.PowerProfileState{
				ActiveProfile:     ev.ActiveProfile,
				AvailableProfiles: profiles,
			},
		},
	}

	for ch := range s.systemSubscribers {
		select {
		case ch <- resp:
		default:
		}
	}
}

// ==========================================
// Bluetooth Service Implementation (Stub)
// ==========================================

func (s *Server) StreamBluetoothState(
	ctx context.Context,
	req *connect.Request[pcdv1.StreamBluetoothStateRequest],
	stream *connect.ServerStream[pcdv1.StreamBluetoothStateResponse],
) error {
	// Bluetooth monitor is not implemented in Go daemon engine, so this stream is currently a stub
	<-ctx.Done()
	return nil
}

// ==========================================
// Structural Mapping Helpers
// ==========================================

func mapTelemetryPayload(sys metrics.SystemMetrics) *pcdv1.TelemetryPayload {
	return &pcdv1.TelemetryPayload{
		Timestamp: time.Now().Unix(),
		Cpu: &pcdv1.CPUUsage{
			UsagePercent:      sys.CPU.UsagePercent,
			TempCelsius:       sys.CPU.TempCelsius,
			PowerWatts:        sys.CPU.PowerWatts,
			TmaxCelsius:       0, // unsupported by Go backend currently
			CoresUsagePercent: sys.CPU.CoresUsagePercent,
			FreqMhz:           sys.CPU.FreqMHz,
		},
		Gpu: &pcdv1.GPUUsage{
			UsagePercent:    sys.GPU.UsagePercent,
			TempCelsius:     sys.GPU.TempCelsius,
			VramUsedBytes:   sys.GPU.VramUsedBytes,
			VramTotalBytes:  sys.GPU.VramTotalBytes,
			PowerWatts:      sys.GPU.PowerWatts,
			VramTempCelsius: sys.GPU.VramTempCelsius,
			VramFreqMhz:     sys.GPU.VramFreqMHz,
			TmaxCelsius:     0, // unsupported by Go backend currently
			FreqMhz:         sys.GPU.FreqMHz,
		},
		Ram: &pcdv1.RAMUsage{
			UsedBytes:  sys.RAM.UsedBytes,
			TotalBytes: sys.RAM.TotalBytes,
			Percentage: sys.RAM.Percentage,
		},
		Swap: &pcdv1.SwapUsage{
			UsedBytes:  sys.Swap.UsedBytes,
			TotalBytes: sys.Swap.TotalBytes,
			Percentage: sys.Swap.Percentage,
		},
		Zram: &pcdv1.ZRAMUsage{
			OrigDataSizeBytes:  sys.ZRAM.OrigDataSizeBytes,
			ComprDataSizeBytes: sys.ZRAM.ComprDataSizeBytes,
			MemUsedTotalBytes:  sys.ZRAM.MemUsedTotalBytes,
			TotalBytes:         sys.ZRAM.TotalBytes,
			CompressionRatio:   sys.ZRAM.CompressionRatio,
		},
		Flags: &pcdv1.TelemetryFlags{
			CpuUsageSupported:       sys.Flags.CPUUsageSupported,
			CpuCoresUsageSupported:  sys.Flags.CPUCoresUsageSupported,
			CpuTempSupported:        sys.Flags.CPUTempSupported,
			CpuFreqSupported:        sys.Flags.CPUFreqSupported,
			CpuPowerSupported:       sys.Flags.CPUPowerSupported,
			CpuTempTmaxSupported:    false,
			RamSupported:            sys.Flags.RAMSupported,
			SwapSupported:           sys.Flags.SwapSupported,
			ZramSupported:           sys.Flags.ZRAMSupported,
			GpuSupported:            sys.Flags.GPUSupported,
			GpuUsageSupported:       sys.Flags.GPUUsageSupported,
			GpuTempSupported:        sys.Flags.GPUTempSupported,
			GpuVramSupported:        sys.Flags.GPUVramSupported,
			GpuFreqSupported:        sys.Flags.GPUFreqSupported,
			GpuPowerSupported:       sys.Flags.GPUPowerSupported,
			GpuVramTempSupported:    sys.Flags.GPUVramTempSupported,
			GpuVramFreqSupported:    sys.Flags.GPUVramFreqSupported,
			GpuTempTmaxSupported:    false,
			OsdSupported:            false,
			PeripheralsSupported:    false,
			PackageUpdatesSupported: false,
		},
	}
}

func mapNotificationEvent(ev notifications.NotificationEvent) *pcdv1.NotificationEvent {
	rawIcon, url := decodeBase64Image(ev.AppIconBase64)
	icon := ev.AppIcon
	if url == "" && icon == "" {
		icon = "dialog-information"
	}

	var hintsStruct *structpb.Struct
	if ev.Hints != nil {
		h, err := structpb.NewStruct(ev.Hints)
		if err == nil {
			hintsStruct = h
		}
	}

	return &pcdv1.NotificationEvent{
		Id:            ev.ReplacesID, // Correlates to notification systems
		AppName:       ev.AppName,
		ReplacesId:    ev.ReplacesID,
		AppIcon:       icon,
		AppIconRaw:    rawIcon,
		Summary:       ev.Summary,
		Body:          ev.Body,
		Actions:       ev.Actions,
		Hints:         hintsStruct,
		ExpireTimeout: ev.ExpireTimeout,
	}
}

func mapMediaEvent(ev mpris.MediaEvent) *pcdv1.MediaStatePayload {
	players := make([]*pcdv1.MediaPlayerState, len(ev.ActivePlayers))
	for i, p := range ev.ActivePlayers {
		rawArt, url := decodeBase64Image(p.Metadata.ArtURL)

		players[i] = &pcdv1.MediaPlayerState{
			PlayerName:           p.PlayerName,
			Identity:             p.Identity,
			DesktopEntry:         p.DesktopEntry,
			PlaybackStatus:       string(p.PlaybackStatus),
			Volume:               p.Volume,
			PositionMicroseconds: p.PositionMicro,
			Metadata: &pcdv1.MediaTrackMetadata{
				TrackId:            p.Metadata.TrackID,
				Title:              p.Metadata.Title,
				Artist:             p.Metadata.Artist,
				Album:              p.Metadata.Album,
				ArtUrl:             url,
				ArtRaw:             rawArt,
				LengthMicroseconds: p.Metadata.LengthMicro,
			},
		}
	}
	return &pcdv1.MediaStatePayload{ActivePlayers: players}
}

func decodeBase64Image(dataURL string) ([]byte, string) {
	if dataURL == "" {
		return nil, ""
	}
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, dataURL // Normal HTTP/HTTPS url, keep URL, no raw bytes
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) < 2 {
		return nil, dataURL
	}
	header := parts[0]
	body := parts[1]
	if !strings.Contains(header, ";base64") {
		return nil, dataURL
	}
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, dataURL
	}
	return decoded, "" // Base64 inline decoded, return raw bytes and clear URL
}
