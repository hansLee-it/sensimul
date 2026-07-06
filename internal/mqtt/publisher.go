package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog"
)

// Options captures runtime MQTT connection and publish behavior.
type Options struct {
	BrokerURL string
	ClientID  string
	QoS       byte
	Retain    bool
}

// subscription records a topic + handler so it can be (re)applied on every
// connection, including automatic reconnects.
type subscription struct {
	topic   string
	handler func(topic string, payload []byte)
}

// Publisher wraps MQTT client lifecycle and topic conventions.
type Publisher struct {
	client paho.Client
	opts   Options
	logger zerolog.Logger

	mu        sync.Mutex
	subs      []subscription
	onConnect func()
}

func NewPublisher(opts Options, logger zerolog.Logger) *Publisher {
	return &Publisher{
		opts:   opts,
		logger: logger,
	}
}

func (p *Publisher) Connect(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.opts.BrokerURL == "" {
		return fmt.Errorf("mqtt broker_url is empty")
	}
	if p.opts.ClientID == "" {
		p.opts.ClientID = fmt.Sprintf("sensimul-%d", time.Now().UnixNano())
	}

	options := paho.NewClientOptions().
		AddBroker(p.opts.BrokerURL).
		SetClientID(p.opts.ClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		// paho defaults to CleanSession=true / ResumeSubs=false, so subscriptions
		// are NOT restored by the broker on reconnect. Re-apply them ourselves on
		// every (re)connect so command/test reception resumes after a broker
		// restart or network blip. This runs for the initial connect too.
		SetOnConnectHandler(func(_ paho.Client) {
			p.handleConnect()
		})

	p.client = paho.NewClient(options)
	token := p.client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("mqtt connect timeout")
	}
	if err := token.Error(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	p.logger.Info().Str("broker", p.opts.BrokerURL).Msg("mqtt connected")
	return nil
}

func (p *Publisher) PublishSensor(ctx context.Context, siteID, sensorID string, payload []byte) error {
	if p == nil {
		return nil
	}
	if p.client == nil || !p.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt not connected")
	}

	topic := TopicLiveSensor(siteID, sensorID)
	token := p.client.Publish(topic, p.opts.QoS, p.opts.Retain, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout: %s", topic)
	}
	if err := token.Error(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return nil
}

func (p *Publisher) PublishTestRequest(ctx context.Context, req SensorTestRequest) error {
	if p == nil {
		return nil
	}
	if p.client == nil || !p.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt not connected")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal test request: %w", err)
	}

	topic := TopicTestRequest(req.SiteID, req.SensorID)
	token := p.client.Publish(topic, p.opts.QoS, false, body)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout: %s", topic)
	}
	if err := token.Error(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return nil
}

func (p *Publisher) PublishTestResult(ctx context.Context, result SensorTestResult) error {
	if p == nil {
		return nil
	}
	if p.client == nil || !p.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt not connected")
	}

	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal test result: %w", err)
	}

	topic := TopicTestResult(result.SiteID, result.SensorID)
	token := p.client.Publish(topic, p.opts.QoS, false, body)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout: %s", topic)
	}
	if err := token.Error(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return nil
}

// PublishControllerAck publishes a command ACK back to the API server.
func (p *Publisher) PublishControllerAck(ctx context.Context, siteID, controllerID string, ack ControllerCommandAck) error {
	if p == nil {
		return nil
	}
	if p.client == nil || !p.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt not connected")
	}

	body, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("marshal controller ack: %w", err)
	}

	topic := TopicControllerAck(siteID, controllerID)
	token := p.client.Publish(topic, p.opts.QoS, false, body)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("mqtt publish timeout: %s", topic)
	}
	if err := token.Error(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return nil
}

func (p *Publisher) Subscribe(ctx context.Context, topic string, handler func(topic string, payload []byte)) error {
	if p == nil {
		return nil
	}
	if p.client == nil || !p.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt not connected")
	}

	sub := subscription{topic: topic, handler: handler}

	// Record the subscription so it is re-applied on every reconnect (see
	// SetOnConnectHandler in Connect).
	p.mu.Lock()
	p.subs = append(p.subs, sub)
	p.mu.Unlock()

	if err := p.applySubscription(sub); err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		ut := p.client.Unsubscribe(topic)
		ut.WaitTimeout(3 * time.Second)
	}()

	return nil
}

// AddSubscription records a subscription for replay on every (re)connect without
// requiring an open connection. Use this when subscriptions must survive an
// initial connect failure — the OnConnect handler applies them once the client
// connects (including background retries). Safe to call before Connect.
func (p *Publisher) AddSubscription(topic string, handler func(topic string, payload []byte)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.subs = append(p.subs, subscription{topic: topic, handler: handler})
	p.mu.Unlock()
}

// SetOnConnect registers a callback invoked after subscriptions are (re)applied
// on each successful connection. Used to flip readiness state on (re)connect.
func (p *Publisher) SetOnConnect(cb func()) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.onConnect = cb
	p.mu.Unlock()
}

// applySubscription performs the actual broker subscribe for one recorded entry.
func (p *Publisher) applySubscription(sub subscription) error {
	token := p.client.Subscribe(sub.topic, p.opts.QoS, func(_ paho.Client, msg paho.Message) {
		sub.handler(msg.Topic(), msg.Payload())
	})
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("mqtt subscribe timeout: %s", sub.topic)
	}
	return token.Error()
}

// handleConnect runs on every successful (re)connect: it replays subscriptions
// (paho does not restore them under the default CleanSession) and fires the
// optional readiness callback.
func (p *Publisher) handleConnect() {
	p.resubscribe()
	p.mu.Lock()
	cb := p.onConnect
	p.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// resubscribe replays every recorded subscription. Called from the OnConnect
// handler so subscriptions survive reconnects (paho does not restore them).
func (p *Publisher) resubscribe() {
	if p.client == nil {
		return
	}
	p.mu.Lock()
	subs := make([]subscription, len(p.subs))
	copy(subs, p.subs)
	p.mu.Unlock()

	for _, sub := range subs {
		if err := p.applySubscription(sub); err != nil {
			p.logger.Warn().Err(err).Str("topic", sub.topic).Msg("failed to (re)subscribe on connect")
			continue
		}
		p.logger.Debug().Str("topic", sub.topic).Msg("subscription (re)applied on connect")
	}
}

func (p *Publisher) Close() {
	if p == nil || p.client == nil {
		return
	}
	p.client.Disconnect(200)
	p.logger.Info().Msg("mqtt disconnected")
}
