package auth

import (
	"context"
	"errors"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

// streamFirstTokenBufferLimit is how many pre-token frames may be held before
// the attempt is canceled. Reaching the limit does not commit the stream.
const streamFirstTokenBufferLimit = 64

type streamAttemptOutcome struct {
	result *cliproxyexecutor.StreamResult
	err    error
}

func isStreamFirstTokenTimeout(err error) bool {
	return errors.Is(err, cliproxyexecutor.ErrStreamFirstTokenTimeout)
}

func executeStreamObservingFirstToken(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if executor == nil {
		return nil, &Error{Code: "executor_not_found", Message: "executor not registered"}
	}
	timeout := time.Duration(0)
	if auth != nil {
		timeout = auth.StreamFirstTokenTimeout()
	}
	if timeout <= 0 {
		return executor.ExecuteStream(ctx, auth, req, opts)
	}
	return executeStreamUntilFirstToken(ctx, timeout, cliproxyexecutor.ResponseFormatOrSource(opts), func(attemptCtx context.Context) (*cliproxyexecutor.StreamResult, error) {
		return executor.ExecuteStream(attemptCtx, auth, req, opts)
	})
}

func executeStreamUntilFirstToken(parent context.Context, timeout time.Duration, format sdktranslator.Format, run func(context.Context) (*cliproxyexecutor.StreamResult, error)) (*cliproxyexecutor.StreamResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		return run(parent)
	}
	attemptCtx, cancelAttempt := context.WithCancelCause(parent)
	done := make(chan streamAttemptOutcome, 1)
	go func() {
		result, err := run(attemptCtx)
		done <- streamAttemptOutcome{result: result, err: err}
	}()

	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(timeout)
	var call streamAttemptOutcome
	select {
	case <-parent.Done():
		stopFirstTokenTimer(timer)
		cancelAttempt(context.Canceled)
		call = <-done
		discardObservedStream(call.result)
		return nil, parent.Err()
	case <-timer.C:
		return cancelForFirstTokenTimeout(parent, cancelAttempt, done, timeout)
	case call = <-done:
		stopFirstTokenTimer(timer)
	}

	if parentErr := parent.Err(); parentErr != nil {
		cancelAttempt(context.Canceled)
		discardObservedStream(call.result)
		return nil, parentErr
	}
	if call.err != nil {
		cancelAttempt(context.Canceled)
		discardObservedStream(call.result)
		return nil, call.err
	}
	if call.result == nil || call.result.Chunks == nil {
		cancelAttempt(context.Canceled)
		return nil, &Error{Code: "empty_stream", Message: "upstream stream has no source", Retryable: true}
	}

	buffered, errWait := readUntilFirstStreamToken(parent, call.result.Chunks, deadline, format)
	if errWait != nil {
		if parentErr := parent.Err(); parentErr != nil {
			cancelAttempt(context.Canceled)
			discardStreamChunks(call.result.Chunks)
			return nil, parentErr
		}
		if isStreamFirstTokenTimeout(errWait) {
			logFirstTokenTimeout(parent, timeout)
			cancelAttempt(cliproxyexecutor.ErrStreamFirstTokenTimeout)
			discardStreamChunks(call.result.Chunks)
			return nil, &cliproxyexecutor.StreamFirstTokenTimeoutError{Timeout: timeout}
		}
		cancelAttempt(context.Canceled)
		discardStreamChunks(call.result.Chunks)
		return nil, errWait
	}
	call.result.Chunks = prependStreamChunks(buffered, call.result.Chunks)
	return call.result, nil
}

func cancelForFirstTokenTimeout(parent context.Context, cancelAttempt context.CancelCauseFunc, done <-chan streamAttemptOutcome, timeout time.Duration) (*cliproxyexecutor.StreamResult, error) {
	if parentErr := parent.Err(); parentErr != nil {
		cancelAttempt(context.Canceled)
		call := <-done
		discardObservedStream(call.result)
		return nil, parentErr
	}
	logFirstTokenTimeout(parent, timeout)
	cancelAttempt(cliproxyexecutor.ErrStreamFirstTokenTimeout)
	call := <-done
	discardObservedStream(call.result)
	if parentErr := parent.Err(); parentErr != nil {
		return nil, parentErr
	}
	return nil, &cliproxyexecutor.StreamFirstTokenTimeoutError{Timeout: timeout}
}

func logFirstTokenTimeout(ctx context.Context, timeout time.Duration) {
	logEntryWithRequestID(ctx).Debugf("stream first token timeout after %s; canceling upstream attempt", timeout)
}

func stopFirstTokenTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func discardObservedStream(result *cliproxyexecutor.StreamResult) {
	if result == nil {
		return
	}
	discardStreamChunks(result.Chunks)
}

func readUntilFirstStreamToken(parent context.Context, chunks <-chan cliproxyexecutor.StreamChunk, deadline time.Time, format sdktranslator.Format) ([]cliproxyexecutor.StreamChunk, error) {
	if chunks == nil {
		return nil, &Error{Code: "empty_stream", Message: "upstream stream has no source", Retryable: true}
	}
	wait := time.Until(deadline)
	if wait < 0 {
		wait = 0
	}
	timer := time.NewTimer(wait)
	defer stopFirstTokenTimer(timer)
	buffered := make([]cliproxyexecutor.StreamChunk, 0, 4)
	for {
		if len(buffered) >= streamFirstTokenBufferLimit {
			return nil, &cliproxyexecutor.StreamFirstTokenTimeoutError{Timeout: time.Until(deadline)}
		}
		var chunk cliproxyexecutor.StreamChunk
		var ok bool
		select {
		case <-parent.Done():
			return nil, parent.Err()
		case <-timer.C:
			return nil, &cliproxyexecutor.StreamFirstTokenTimeoutError{Timeout: time.Until(deadline)}
		case chunk, ok = <-chunks:
		}
		if !ok {
			return nil, &Error{Code: "empty_stream", Message: "upstream stream closed before first token", Retryable: true}
		}
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		buffered = append(buffered, chunk)
		if cliproxyexecutor.IsStreamTokenPayload(format, chunk.Payload) {
			return buffered, nil
		}
	}
}

func prependStreamChunks(buffered []cliproxyexecutor.StreamChunk, remaining <-chan cliproxyexecutor.StreamChunk) <-chan cliproxyexecutor.StreamChunk {
	out := make(chan cliproxyexecutor.StreamChunk, len(buffered)+1)
	go func() {
		defer close(out)
		for _, chunk := range buffered {
			out <- chunk
		}
		if remaining == nil {
			return
		}
		for chunk := range remaining {
			out <- chunk
		}
	}()
	return out
}
