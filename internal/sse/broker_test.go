package sse

import "testing"

func TestBrokerDeliversToSubscribers(t *testing.T) {
	b := NewBroker()
	ch1, cancel1 := b.Subscribe("m1")
	defer cancel1()
	ch2, cancel2 := b.Subscribe("m1")
	defer cancel2()
	other, cancelOther := b.Subscribe("m2")
	defer cancelOther()

	b.Publish("m1")

	for i, ch := range []<-chan struct{}{ch1, ch2} {
		select {
		case <-ch:
		default:
			t.Fatalf("subscriber %d got no signal", i+1)
		}
	}
	select {
	case <-other:
		t.Fatal("m2 subscriber must not receive m1 events")
	default:
	}
}

func TestBrokerPublishNeverBlocks(t *testing.T) {
	b := NewBroker()
	_, cancel := b.Subscribe("m1")
	defer cancel()
	b.Publish("m1")
	b.Publish("m1")
	b.Publish("m1")
}

func TestBrokerPublishWithoutSubscribers(t *testing.T) {
	NewBroker().Publish("ghost") // must not panic
}

func TestBrokerUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe("m1")
	cancel()
	b.Publish("m1")
	select {
	case <-ch:
		t.Fatal("cancelled subscriber still receives")
	default:
	}
	cancel() // double-cancel must be safe
}
