// Copyright 2023 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package eventmultiplexer

import (
	"context"
	"sync"
	stdtesting "testing"
	"time"

	"github.com/juju/tc"
	"github.com/juju/worker/v5/workertest"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/juju/juju/core/changestream"
	changestreamtesting "github.com/juju/juju/core/changestream/testing"
	"github.com/juju/juju/core/database"
	coretrace "github.com/juju/juju/core/trace"
	"github.com/juju/juju/core/testing"
	loggertesting "github.com/juju/juju/internal/logger/testing"
)

const (
	// We need to ensure that we never witness a change term. We've picked
	// an arbitrary timeout of 1 second, which should be more than enough
	// time for the worker to process the change.
	witnessChangeLongDuration  = time.Second
	witnessChangeShortDuration = witnessChangeLongDuration / 2
)

type eventMultiplexerSuite struct {
	baseSuite
}

func TestEventMultiplexerSuite(t *stdtesting.T) {
	defer goleak.VerifyNone(t)
	tc.Run(t, &eventMultiplexerSuite{})
}

func (s *eventMultiplexerSuite) TestSubscribe(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).AnyTimes()

	s.metrics.EXPECT().SubscriptionsInc()

	// This confirms the unsubscription invoked by killing the sub.
	s.metrics.EXPECT().SubscriptionsDec()

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
	c.Assert(err, tc.ErrorIsNil)

	// Kill, then bump the loop so it comes around to the top and cleans up.
	sub.Kill()
	queue.Report(c.Context())
}

func (s *eventMultiplexerSuite) TestDispatch(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	s.metrics.EXPECT().SubscriptionsInc()
	// There is a race between loop select completion and worker Kill.
	// Killing the worker kills the subs attached to its catacomb,
	// So they might or might not be dead when we come back to the top
	// the loop and clean up.
	s.metrics.EXPECT().SubscriptionsDec().MaxTimes(1)
	s.clock.EXPECT().Now().MinTimes(1)
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), false)

	sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
	c.Assert(err, tc.ErrorIsNil)

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	})
	s.dispatchTerm(c, terms)

	var changes []changestream.ChangeEvent
	select {
	case changes = <-sub.Changes():
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for event")
	}

	c.Assert(changes, tc.HasLen, 1)
	c.Check(changes[0].Type(), tc.DeepEquals, changestreamtesting.Create)
	c.Check(changes[0].Namespace(), tc.DeepEquals, "topic")
	c.Check(changes[0].Changed(), tc.Equals, "1")
}

func (s *eventMultiplexerSuite) TestMultipleDispatch(c *tc.C) {
	s.testMultipleDispatch(c, changestream.Namespace("topic", changestreamtesting.Update))
}

func (s *eventMultiplexerSuite) TestMultipleDispatchWithNoOptions(c *tc.C) {
	s.testMultipleDispatch(c)
}

func (s *eventMultiplexerSuite) TestMultipleDispatchWithMultipleMasks(c *tc.C) {
	s.testMultipleDispatch(c, changestream.Namespace("topic", changestreamtesting.Create|changestreamtesting.Update))
}

func (s *eventMultiplexerSuite) TestMultipleDispatchWithMultipleOptions(c *tc.C) {
	s.testMultipleDispatch(c, changestream.Namespace("topic", changestreamtesting.Update), changestream.Namespace("topic", changestreamtesting.Create))
}

func (s *eventMultiplexerSuite) TestMultipleDispatchWithOverlappingOptions(c *tc.C) {
	s.testMultipleDispatch(c, changestream.Namespace("topic", changestreamtesting.Update), changestream.Namespace("topic", changestreamtesting.Update|changestreamtesting.Create))
}

func (s *eventMultiplexerSuite) TestMultipleDispatchWithDuplicateOptions(c *tc.C) {
	s.testMultipleDispatch(c, changestream.Namespace("topic", changestreamtesting.Update), changestream.Namespace("topic", changestreamtesting.Update))
}

func (s *eventMultiplexerSuite) TestSubscribeWithMatchingFilter(c *tc.C) {
	s.testMultipleDispatch(c, changestream.FilteredNamespace("topic", changestreamtesting.Update, func(event changestream.ChangeEvent) bool {
		return event.Namespace() == "topic"
	}))
}

func (s *eventMultiplexerSuite) testMultipleDispatch(c *tc.C, opts ...changestream.SubscriptionOption) {
	defer s.setupMocks(c).Finish()

	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc().Times(10)
	// There is a race between loop select completion and worker Kill.
	// Killing the worker kills the subs attached to its catacomb,
	// So they might or might not be dead when we come back to the top
	// the loop and clean up.
	s.metrics.EXPECT().SubscriptionsDec().MaxTimes(10)
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), false)

	s.clock.EXPECT().Now().MinTimes(1)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.DirtyKill(c, queue)

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Update,
		ns:      "topic",
		changed: "1",
	})

	subs := make([]changestream.Subscription, 10)
	for i := range subs {
		sub, err := queue.Subscribe("foo", opts...)
		c.Assert(err, tc.ErrorIsNil)

		subs[i] = sub
	}

	done := s.dispatchTerm(c, terms)
	select {
	case <-done:
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for dispatching event")
	}

	// The subscriptions are guaranteed to be out of order, so we need to just
	// wait on them all, and then check that they all got the event.
	wg := newWaitGroup(uint64(len(subs)))
	for i, sub := range subs {
		go func(i int, sub changestream.Subscription) {
			defer wg.Done()

			events := <-sub.Changes()
			c.Assert(events, tc.HasLen, 1)
			c.Check(events[0].Type(), tc.DeepEquals, changestreamtesting.Update)
			c.Check(events[0].Namespace(), tc.DeepEquals, "topic")
		}(i, sub)
	}

	select {
	case <-wg.Wait():
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for all events")
	}

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestTopicDoesNotMatch(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc()

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.DirtyKill(c, queue)

	sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
	c.Assert(err, tc.ErrorIsNil)

	s.expectEmptyTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "foo",
		changed: "1",
	})
	done := s.dispatchTerm(c, terms)
	select {
	case <-done:
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for event")
	}

	select {
	case <-sub.Changes():
		c.Fatal("witnessed change when expected none")
	case <-time.After(witnessChangeShortDuration):
	}

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestTopicMatchesOne(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc().Times(2)
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), false)

	s.clock.EXPECT().Now().MinTimes(1)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.DirtyKill(c, queue)

	sub0, err := queue.Subscribe("foo", changestream.Namespace("foo", changestreamtesting.Create))
	c.Assert(err, tc.ErrorIsNil)

	sub1, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
	c.Assert(err, tc.ErrorIsNil)

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	})
	done := s.dispatchTerm(c, terms)
	select {
	case <-done:
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for event")
	}

	select {
	case <-sub1.Changes():
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for event")
	}

	select {
	case <-sub0.Changes():
		c.Fatal("witnessed change on sub0 when expected none")
	case <-time.After(witnessChangeShortDuration):
	}

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestSubscriptionDoneWhenEventQueueKilled(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc()
	s.metrics.EXPECT().SubscriptionsDec()
	s.clock.EXPECT().Now().MinTimes(1)
	// We might encounter a dispatch error, therefore we cannot hard-code
	// a false on the second argument of DispatchDurationObserve.
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), gomock.Any())

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
	c.Assert(err, tc.ErrorIsNil)

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	})
	done := s.dispatchTerm(c, terms)
	select {
	case <-done:
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for event")
	}

	// There is no-one reading the subscription's changes channel.
	// The dispatch call will be waiting for the read,
	// so this is a mid-flight termination.
	workertest.CleanKill(c, queue)

	// Killing the queue should kill the subscription.
	select {
	case <-sub.Done():
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for event")
	}
}

func (s *eventMultiplexerSuite) TestUnsubscribeOfOtherSubscription(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc().Times(2)
	s.metrics.EXPECT().SubscriptionsDec().Times(2)
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), gomock.Any())

	s.clock.EXPECT().Now().MinTimes(1)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.DirtyKill(c, queue)

	subs := make([]changestream.Subscription, 2)
	for i := range subs {
		sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
		c.Assert(err, tc.ErrorIsNil)
		subs[i] = sub
	}

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	})
	s.dispatchTerm(c, terms)

	// Whichever subscription receives the event first will kill the other.
	// We wait on them all to either get the event or to be done.
	wg := newWaitGroup(uint64(len(subs)))
	for i, sub := range subs {
		go func(i int, sub changestream.Subscription) {
			defer wg.Done()

			select {
			case <-sub.Changes():
				subs[len(subs)-1-i].Kill()
			case <-sub.Done():
				subs[len(subs)-1-i].Kill()
			}
		}(i, sub)
	}

	select {
	case <-wg.Wait():
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for all events")
	}

	// Bump the loop so it comes around to the top and cleans up.
	queue.Report(c.Context())

	for _, sub := range subs {
		select {
		case <-sub.Done():
		case <-time.After(testing.LongWait):
			c.Fatal("timed out waiting for event")
		}
	}

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestUnsubscribeOfOtherSubscriptionInAnotherGoroutine(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc().Times(2)
	s.metrics.EXPECT().SubscriptionsDec().Times(2)
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), gomock.Any())
	s.clock.EXPECT().Now().MinTimes(1)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.DirtyKill(c, queue)

	subs := make([]changestream.Subscription, 2)
	for i := range subs {

		sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
		c.Assert(err, tc.ErrorIsNil)
		subs[i] = sub
	}

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	})
	s.dispatchTerm(c, terms)

	// Whichever subscription receives the event first will kill the other.
	// We wait on them all to either get the event or to be done.
	wg := newWaitGroup(uint64(len(subs)))
	for i, sub := range subs {
		go func(sub changestream.Subscription, i int) {
			select {
			case <-sub.Changes():
				go func() {
					subs[len(subs)-1-i].Kill()
					wg.Done()
				}()
			case <-sub.Done():
				go func() {
					subs[len(subs)-1-i].Kill()
					wg.Done()
				}()
			}
		}(sub, i)
	}

	select {
	case <-wg.Wait():
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for all events")
	}

	// Bump the loop so it comes around to the top and cleans up.
	queue.Report(c.Context())

	for _, sub := range subs {
		select {
		case <-sub.Done():
		case <-time.After(testing.LongWait):
			c.Fatal("timed out waiting for event")
		}
	}

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestUnsubscribeOnDispatchTimeout(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc()

	// This is important. We should see this occur as a result of the
	// Unsubscribe call.
	s.metrics.EXPECT().SubscriptionsDec()
	s.clock.EXPECT().Now().AnyTimes()

	// The dispatch should be observed as a failure.
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), true).AnyTimes()

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	sub, err := queue.Subscribe("foo")
	c.Assert(err, tc.ErrorIsNil)

	// Shorten the dispatch timeout in order to trigger Unsubscribe sooner.
	sub.(*subscription).dispatchTimeout = testing.ShortWait

	s.term.EXPECT().Changes().Return([]changestream.ChangeEvent{changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	}})

	// We are not reading the subscription's changes channel,
	// but we expect the sub to be cancelled and the term dispatch completed.
	s.term.EXPECT().Done(false, gomock.Any())
	s.dispatchTerm(c, terms)

	// The subscription should have been unsubscribed due to the timeout.
	select {
	case <-sub.Done():
	case <-time.After(testing.LongWait):
		c.Fatalf("timed out waiting for subscription to be done")
	}
}

func (s *eventMultiplexerSuite) TestStreamDying(c *tc.C) {
	defer s.setupMocks(c).Finish()

	ch := make(chan struct{})
	s.expectStreamDying(ch)

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc().Times(2)
	s.clock.EXPECT().Now().MinTimes(2)
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), false)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.DirtyKill(c, queue)

	subs := make([]changestream.Subscription, 2)
	for i := range subs {
		sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
		c.Assert(err, tc.ErrorIsNil)
		subs[i] = sub
	}

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	})
	s.dispatchTerm(c, terms)

	// The subscriptions are guaranteed to be out of order, so we need to just
	// wait on them all, and then check that they all got the event.
	wg := newWaitGroup(uint64(len(subs)))
	for i, sub := range subs {
		go func(sub changestream.Subscription, i int) {
			<-sub.Changes()
			go func() {
				defer wg.Done()
			}()
		}(sub, i)
	}

	select {
	case <-wg.Wait():
		close(ch)

	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for all events")
	}

	// Give a grace period for the stream to die and then kill the queue.
	// This should ensure that all the subscriptions are cleaned up.
	<-time.After(testing.ShortWait)
	workertest.CleanKill(c, queue)

	for _, sub := range subs {
		select {
		case <-sub.Done():
		case <-time.After(testing.LongWait):
			c.Fatal("timed out waiting for event")
		}
	}
}

func (s *eventMultiplexerSuite) TestStreamDyingWhilstDispatching(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	ch := make(chan struct{})
	s.expectStreamDying(ch)

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	s.metrics.EXPECT().SubscriptionsInc().Times(2)
	s.clock.EXPECT().Now().MinTimes(1)
	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), false)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	subs := make([]changestream.Subscription, 2)
	for i := range subs {
		sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
		c.Assert(err, tc.ErrorIsNil)
		subs[i] = sub
	}

	s.expectTerm(c, changeEvent{
		ctype:   changestreamtesting.Create,
		ns:      "topic",
		changed: "1",
	})
	s.dispatchTerm(c, terms)

	var once sync.Once

	// The subscriptions are guaranteed to be out of order, so we need to just
	// wait on them all, and then check that they all got the event.
	wg := newWaitGroup(uint64(len(subs)))
	for i, sub := range subs {
		go func(sub changestream.Subscription, i int) {
			_, ok := <-sub.Changes()
			if !ok {
				wg.Done()
				return
			}

			go func() {
				defer wg.Done()

				// This will cause a race to close the channel, but that's
				// fine, as we're only interested in the first one.
				once.Do(func() {
					close(ch)
				})

			}()
		}(sub, i)
	}

	select {
	case <-wg.Wait():
	case <-time.After(testing.ShortWait):
		c.Fatal("timed out waiting for all events")
	}

	// Give a grace period for the stream to die and then kill the queue. This
	// should ensure that all the subscriptions are cleaned up.
	<-time.After(testing.ShortWait)
	workertest.CleanKill(c, queue)

	for _, sub := range subs {
		select {
		case <-sub.Done():
		case <-time.After(testing.LongWait):
			c.Fatal("timed out waiting for event")
		}
	}
}

func (s *eventMultiplexerSuite) TestStreamDyingOnStartup(c *tc.C) {
	defer s.setupMocks(c).Finish()

	ch := make(chan struct{})
	s.expectStreamDying(ch)

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	close(ch)

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestStreamDyingOnSubscribe(c *tc.C) {
	defer s.setupMocks(c).Finish()

	ch := make(chan struct{})
	s.expectStreamDying(ch)

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)

	// We don't care for the metrics recording here, as we might not
	// have recorded the metrics in time before dying.
	s.metrics.EXPECT().SubscriptionsInc().AnyTimes()
	s.metrics.EXPECT().SubscriptionsDec().AnyTimes()

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	close(ch)

	// Give a grace period for the stream to die and then kill the queue. This
	// should ensure that all the subscriptions are cleaned up.
	<-time.After(testing.ShortWait)
	workertest.CleanKill(c, queue)

	sub, err := queue.Subscribe("foo")
	c.Assert(err, tc.ErrorIs, database.ErrEventMultiplexerDying)
	c.Check(sub, tc.IsNil)
}

func (s *eventMultiplexerSuite) TestReportWithAllSubscriptions(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)
	s.clock.EXPECT().Now().AnyTimes()

	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), gomock.Any()).AnyTimes()
	s.metrics.EXPECT().SubscriptionsInc().Times(10)
	s.metrics.EXPECT().SubscriptionsDec().Times(10)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	var subs []changestream.Subscription
	for range 10 {
		sub, err := queue.Subscribe("foo")
		c.Assert(err, tc.ErrorIsNil)
		subs = append(subs, sub)
	}

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        10,
		"subscriptions-by-ns":  0,
		"subscriptions-all":    10,
		"dispatch-error-count": 0,
	})

	for _, sub := range subs {
		sub.Kill()
	}

	// Bump the loop so it comes around to the top and cleans up dead subs.
	queue.Report(c.Context())

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        0,
		"subscriptions-by-ns":  0,
		"subscriptions-all":    0,
		"dispatch-error-count": 0,
	})

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestReportWithTopicSubscriptions(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)
	s.clock.EXPECT().Now().AnyTimes()

	s.metrics.EXPECT().SubscriptionsInc().Times(10)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	var subs []changestream.Subscription
	for range 10 {
		sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
		c.Assert(err, tc.ErrorIsNil)

		subs = append(subs, sub)
	}

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        len(subs),
		"subscriptions-by-ns":  1,
		"subscriptions-all":    0,
		"dispatch-error-count": 0,
	})

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestReportWithMultipleTopicSubscriptions(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)
	s.clock.EXPECT().Now().AnyTimes()

	s.metrics.EXPECT().SubscriptionsInc().Times(10)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	var subs []changestream.Subscription
	for range 10 {
		sub, err := queue.Subscribe(
			"foo",
			changestream.Namespace("topic", changestreamtesting.Create),
			changestream.Namespace("foo", changestreamtesting.Update),
		)
		c.Assert(err, tc.ErrorIsNil)

		subs = append(subs, sub)
	}

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        len(subs),
		"subscriptions-by-ns":  2,
		"subscriptions-all":    0,
		"dispatch-error-count": 0,
	})

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestReportWithDuplicateTopicSubscriptions(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)
	s.clock.EXPECT().Now().AnyTimes()

	s.metrics.EXPECT().SubscriptionsInc().Times(10)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	var subs []changestream.Subscription
	for range 10 {
		sub, err := queue.Subscribe(
			"foo",
			changestream.Namespace("topic", changestreamtesting.Update),
			changestream.Namespace("topic", changestreamtesting.Update),
		)
		c.Assert(err, tc.ErrorIsNil)

		subs = append(subs, sub)
	}

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        len(subs),
		"subscriptions-by-ns":  1,
		"subscriptions-all":    0,
		"dispatch-error-count": 0,
	})

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestReportWithMultipleDuplicateTopicSubscriptions(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)
	s.clock.EXPECT().Now().AnyTimes()

	s.metrics.EXPECT().SubscriptionsInc().Times(10)

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	var subs []changestream.Subscription
	for range 10 {
		sub, err := queue.Subscribe(
			"foo",
			changestream.Namespace("topic", changestreamtesting.Create),
			changestream.Namespace("topic", changestreamtesting.Update),
		)
		c.Assert(err, tc.ErrorIsNil)

		subs = append(subs, sub)
	}

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        len(subs),
		"subscriptions-by-ns":  1,
		"subscriptions-all":    0,
		"dispatch-error-count": 0,
	})

	workertest.CleanKill(c, queue)
}

func (s *eventMultiplexerSuite) TestReportWithTopicRemovalAfterUnsubscribe(c *tc.C) {
	defer s.setupMocks(c).Finish()

	s.expectAfter()
	s.expectStreamDying(make(<-chan struct{}))

	terms := make(chan changestream.Term)
	s.stream.EXPECT().Terms().Return(terms).MinTimes(1)
	s.clock.EXPECT().Now().AnyTimes()

	s.metrics.EXPECT().DispatchDurationObserve(gomock.Any(), gomock.Any()).AnyTimes()
	s.metrics.EXPECT().SubscriptionsInc()
	s.metrics.EXPECT().SubscriptionsDec()

	queue, err := New(s.stream, s.clock, s.metrics, loggertesting.WrapCheckLog(c), 0)
	c.Assert(err, tc.ErrorIsNil)
	defer workertest.CleanKill(c, queue)

	sub, err := queue.Subscribe("foo", changestream.Namespace("topic", changestreamtesting.Create))
	c.Assert(err, tc.ErrorIsNil)

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        1,
		"subscriptions-by-ns":  1,
		"subscriptions-all":    0,
		"dispatch-error-count": 0,
	})

	sub.Kill()

	// Bump the loop so it comes around to the top and cleans up dead subs.
	queue.Report(c.Context())

	c.Check(queue.Report(c.Context()), tc.DeepEquals, map[string]any{
		"subscriptions":        0,
		"subscriptions-by-ns":  0,
		"subscriptions-all":    0,
		"dispatch-error-count": 0,
	})

	workertest.CleanKill(c, queue)
}

// tracedChangeEvent is a change event with configurable trace
// and span IDs for use in tests.
type tracedChangeEvent struct {
	changeEvent
	traceID string
	spanID  string
}

func (e tracedChangeEvent) TraceID() string { return e.traceID }
func (e tracedChangeEvent) SpanID() string  { return e.spanID }

// spyLink records a single AddLink invocation.
type spyLink struct {
	traceID string
	spanID  string
}

// spySpan is a span that records AddLink calls.
type spySpan struct {
	coretrace.NoopSpan
	links []spyLink
}

func (s *spySpan) AddLink(traceID, spanID string) {
	s.links = append(s.links, spyLink{
		traceID: traceID,
		spanID:  spanID,
	})
}

// spyTracer returns the fixed spySpan on every Start call
// and reports itself as enabled so the production code uses it.
type spyTracer struct {
	span *spySpan
}

func (t *spyTracer) Start(
	ctx context.Context,
	_ string,
	_ ...coretrace.Option,
) (context.Context, coretrace.Span) {
	return ctx, t.span
}

func (t *spyTracer) Enabled() bool { return true }

type resolveTraceContextSuite struct{}

func TestResolveTraceContextSuite(t *stdtesting.T) {
	defer goleak.VerifyNone(t)
	tc.Run(t, &resolveTraceContextSuite{})
}

func (s *resolveTraceContextSuite) TestEmptyTraceIDs(c *tc.C) {
	changes := ChangeSet{
		changeEvent{
			ctype:   changestreamtesting.Create,
			ns:      "foo",
			changed: "1",
		},
		changeEvent{
			ctype:   changestreamtesting.Update,
			ns:      "foo",
			changed: "2",
		},
	}
	spy := &spySpan{}
	ctx := coretrace.WithTracer(c.Context(), &spyTracer{span: spy})
	result := resolveTraceContext(ctx, changes)
	c.Check(result, tc.DeepEquals, changes)
	c.Check(spy.links, tc.HasLen, 0)
}

func (s *resolveTraceContextSuite) TestUniformTraceIDs(c *tc.C) {
	changes := ChangeSet{
		tracedChangeEvent{
			changeEvent: changeEvent{
				ctype:   changestreamtesting.Create,
				ns:      "foo",
				changed: "1",
			},
			traceID: "aaaa",
			spanID:  "1111",
		},
		tracedChangeEvent{
			changeEvent: changeEvent{
				ctype:   changestreamtesting.Update,
				ns:      "foo",
				changed: "2",
			},
			traceID: "aaaa",
			spanID:  "2222",
		},
	}
	spy := &spySpan{}
	ctx := coretrace.WithTracer(c.Context(), &spyTracer{span: spy})
	result := resolveTraceContext(ctx, changes)
	c.Check(result, tc.DeepEquals, changes)
	c.Check(spy.links, tc.HasLen, 0)
}

func (s *resolveTraceContextSuite) TestMixedTraceIDs(c *tc.C) {
	changes := ChangeSet{
		tracedChangeEvent{
			changeEvent: changeEvent{
				ctype:   changestreamtesting.Create,
				ns:      "foo",
				changed: "1",
			},
			traceID: "aaaa",
			spanID:  "1111",
		},
		tracedChangeEvent{
			changeEvent: changeEvent{
				ctype:   changestreamtesting.Update,
				ns:      "foo",
				changed: "2",
			},
			traceID: "bbbb",
			spanID:  "2222",
		},
	}
	spy := &spySpan{}
	ctx := coretrace.WithTracer(c.Context(), &spyTracer{span: spy})
	result := resolveTraceContext(ctx, changes)
	c.Assert(result, tc.HasLen, 2)
	freshID := result[0].TraceID()
	// Fresh W3C trace ID must be 32 lower-case hex characters.
	c.Check(len(freshID), tc.Equals, 32)
	c.Check(freshID, tc.Not(tc.Equals), "aaaa")
	c.Check(freshID, tc.Not(tc.Equals), "bbbb")
	// All results carry the same fresh trace ID.
	c.Check(result[1].TraceID(), tc.Equals, freshID)
	// Last non-empty spanID in the input slice is "2222".
	c.Check(result[0].SpanID(), tc.Equals, "2222")
	c.Check(result[1].SpanID(), tc.Equals, "2222")
	// AddLink must be called once per distinct (traceID, spanID) pair.
	c.Assert(spy.links, tc.HasLen, 2)
	c.Check(spy.links[0], tc.DeepEquals, spyLink{
		traceID: "aaaa",
		spanID:  "1111",
	})
	c.Check(spy.links[1], tc.DeepEquals, spyLink{
		traceID: "bbbb",
		spanID:  "2222",
	})
}
