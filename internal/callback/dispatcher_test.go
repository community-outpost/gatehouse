package callback

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestDispatchUsesBackendAfterEnsuringUser(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	forwarder := &fakeForwarder{accepts: true}
	service := NewService(store, forwarder, discardLogger())

	delivery, err := service.Dispatch(context.Background(), testCallback(), "request-1", "incoming-secret")
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if delivery != DeliveryBackend {
		t.Fatalf("delivery = %q", delivery)
	}
	if store.userUpdates != 1 || store.pendingWrites != 0 || forwarder.attempts != 1 {
		t.Fatalf("updates=%d pending=%d attempts=%d", store.userUpdates, store.pendingWrites, forwarder.attempts)
	}
}

func TestDispatchFallsBackWhenBackendDoesNotAccept(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	forwarder := &fakeForwarder{}
	service := NewService(store, forwarder, discardLogger())

	delivery, err := service.Dispatch(context.Background(), testCallback(), "", "incoming-secret")
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if delivery != DeliveryMySQLFallback {
		t.Fatalf("delivery = %q", delivery)
	}
	if store.userUpdates != 1 || store.pendingWrites != 1 || forwarder.attempts != 1 {
		t.Fatalf("updates=%d pending=%d attempts=%d", store.userUpdates, store.pendingWrites, forwarder.attempts)
	}
}

func testCallback() AuthCallback {
	result, err := Parse([]byte(`{"env":"example_alpha","code":"ABC123","user_id":42,"success":true}`), "")
	if err != nil {
		panic(err)
	}
	return result
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeStore struct {
	userUpdates   int
	pendingWrites int
}

func (f *fakeStore) EnsureUser(context.Context, int64) error {
	f.userUpdates++
	return nil
}

func (f *fakeStore) SavePendingLogin(context.Context, AuthCallback) error {
	f.pendingWrites++
	return nil
}

type fakeForwarder struct {
	accepts  bool
	attempts int
}

func (f *fakeForwarder) TryForward(context.Context, AuthCallback, string, string) (bool, error) {
	f.attempts++
	return f.accepts, nil
}
