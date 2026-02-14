package goutbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IsaacDSC/goutbox"
	"github.com/IsaacDSC/goutbox/mocks"
	"go.uber.org/mock/gomock"
)

func TestNew_WithOptions(t *testing.T) {
	tests := []struct {
		name             string
		opts             []goutbox.ServiceOption
		wantLoopInterval time.Duration
	}{
		{
			name:             "default loop interval when no options",
			opts:             nil,
			wantLoopInterval: goutbox.DefaultLoopInterval,
		},
		{
			name:             "custom loop interval",
			opts:             []goutbox.ServiceOption{goutbox.WithLoopInterval(30 * time.Second)},
			wantLoopInterval: 30 * time.Second,
		},
		{
			name:             "default loop interval when zero provided",
			opts:             []goutbox.ServiceOption{goutbox.WithLoopInterval(0)},
			wantLoopInterval: goutbox.DefaultLoopInterval,
		},
		{
			name:             "default loop interval when negative provided",
			opts:             []goutbox.ServiceOption{goutbox.WithLoopInterval(-1 * time.Second)},
			wantLoopInterval: goutbox.DefaultLoopInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mocks.NewMockStore(ctrl)
			svc := goutbox.New(mockStore, tt.opts...)

			if got := svc.GetLoopInterval(); got != tt.wantLoopInterval {
				t.Errorf("GetLoopInterval() = %v, want %v", got, tt.wantLoopInterval)
			}
		})
	}
}

func TestService_Do(t *testing.T) {
	tests := []struct {
		name        string
		eventName   string
		payload     any
		publishErr  error
		setupMock   func(m *mocks.MockStore)
		wantErr     bool
		errContains string
	}{
		{
			name:       "publisher succeeds - no store call",
			eventName:  "test_event",
			payload:    map[string]string{"key": "value"},
			publishErr: nil,
			setupMock:  func(m *mocks.MockStore) {},
			wantErr:    false,
		},
		{
			name:       "publisher fails - task stored successfully",
			eventName:  "test_event",
			payload:    map[string]string{"key": "value"},
			publishErr: errors.New("publish error"),
			setupMock: func(m *mocks.MockStore) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
			},
			wantErr: false,
		},
		{
			name:       "publisher fails - store create fails",
			eventName:  "test_event",
			payload:    map[string]string{"key": "value"},
			publishErr: errors.New("publish error"),
			setupMock: func(m *mocks.MockStore) {
				m.EXPECT().Create(gomock.Any(), gomock.Any()).Return(errors.New("store error"))
			},
			wantErr:     true,
			errContains: "failed to store task error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mocks.NewMockStore(ctrl)
			tt.setupMock(mockStore)

			svc := goutbox.New(mockStore)
			ctx := context.Background()

			publisher := func(ctx context.Context, eventName string, payload any) error {
				return tt.publishErr
			}

			err := svc.Do(ctx, tt.eventName, tt.payload, publisher)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Do() expected error, got nil")
				}
				if tt.errContains != "" && (err == nil || !containsStr(err.Error(), tt.errContains)) {
					t.Errorf("Do() error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("Do() unexpected error: %v", err)
				}
			}
		})
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestService_Do_WithMaxAttempts(t *testing.T) {
	tests := []struct {
		name            string
		opts            []goutbox.DoOption
		wantMaxAttempts int
	}{
		{
			name:            "default max attempts when no options",
			opts:            nil,
			wantMaxAttempts: goutbox.DefaultMaxAttempts,
		},
		{
			name:            "custom max attempts",
			opts:            []goutbox.DoOption{goutbox.WithMaxAttempts(5)},
			wantMaxAttempts: 5,
		},
		{
			name:            "default max attempts when zero provided",
			opts:            []goutbox.DoOption{goutbox.WithMaxAttempts(0)},
			wantMaxAttempts: goutbox.DefaultMaxAttempts,
		},
		{
			name:            "default max attempts when negative provided",
			opts:            []goutbox.DoOption{goutbox.WithMaxAttempts(-1)},
			wantMaxAttempts: goutbox.DefaultMaxAttempts,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mocks.NewMockStore(ctrl)
			mockStore.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, task goutbox.Task) error {
					if task.Config.MaxAttempts != tt.wantMaxAttempts {
						t.Errorf("Task.Config.MaxAttempts = %d, want %d", task.Config.MaxAttempts, tt.wantMaxAttempts)
					}
					return nil
				},
			)

			svc := goutbox.New(mockStore)
			ctx := context.Background()

			publisher := func(ctx context.Context, eventName string, payload any) error {
				return errors.New("fail to trigger store")
			}

			_ = svc.Do(ctx, "test_event", "payload", publisher, tt.opts...)
		})
	}
}

func TestService_StartTasks(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(m *mocks.MockStore)
		publishFn goutbox.Publisher
		wantErr   bool
	}{
		{
			name: "no tasks to process",
			setupMock: func(m *mocks.MockStore) {
				m.EXPECT().GetTasksWithError(gomock.Any()).Return([]goutbox.Task{}, nil)
			},
			publishFn: func(ctx context.Context, eventName string, payload any) error {
				return nil
			},
			wantErr: false,
		},
		{
			name: "task published successfully",
			setupMock: func(m *mocks.MockStore) {
				m.EXPECT().GetTasksWithError(gomock.Any()).Return([]goutbox.Task{
					{Key: "event1", Payload: "data", Config: goutbox.Config{MaxAttempts: 3}, Attempts: 0},
				}, nil)
				m.EXPECT().DiscardTask(gomock.Any(), "event1", true).Return(nil)
			},
			publishFn: func(ctx context.Context, eventName string, payload any) error {
				return nil
			},
			wantErr: false,
		},
		{
			name: "task exceeds max attempts - discarded",
			setupMock: func(m *mocks.MockStore) {
				m.EXPECT().GetTasksWithError(gomock.Any()).Return([]goutbox.Task{
					{Key: "event1", Payload: "data", Config: goutbox.Config{MaxAttempts: 3}, Attempts: 3},
				}, nil)
				m.EXPECT().DiscardTask(gomock.Any(), "event1", false).Return(nil)
			},
			publishFn: func(ctx context.Context, eventName string, payload any) error {
				return nil
			},
			wantErr: false,
		},
		{
			name: "task publish fails - error updated",
			setupMock: func(m *mocks.MockStore) {
				m.EXPECT().GetTasksWithError(gomock.Any()).Return([]goutbox.Task{
					{Key: "event1", Payload: "data", Config: goutbox.Config{MaxAttempts: 3}, Attempts: 0},
				}, nil)
				m.EXPECT().UpdateTaskError(gomock.Any(), gomock.Any()).Return(nil)
			},
			publishFn: func(ctx context.Context, eventName string, payload any) error {
				return errors.New("publish failed")
			},
			wantErr: false,
		},
		{
			name: "get tasks fails",
			setupMock: func(m *mocks.MockStore) {
				m.EXPECT().GetTasksWithError(gomock.Any()).Return(nil, errors.New("db error"))
			},
			publishFn: func(ctx context.Context, eventName string, payload any) error {
				return nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockStore := mocks.NewMockStore(ctrl)
			tt.setupMock(mockStore)

			svc := goutbox.New(mockStore)
			ctx := context.Background()

			err := svc.StartTasksForTest(ctx, tt.publishFn)

			if tt.wantErr {
				if err == nil {
					t.Errorf("StartTasksForTest() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("StartTasksForTest() unexpected error: %v", err)
				}
			}
		})
	}
}
