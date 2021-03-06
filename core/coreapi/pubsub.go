package coreapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	coreiface "github.com/ipfs/go-ipfs/core/coreapi/interface"
	caopts "github.com/ipfs/go-ipfs/core/coreapi/interface/options"

	pstore "gx/ipfs/QmPiemjiKBC9VA7vZF82m4x1oygtg2c2YVqag8PX7dN1BD/go-libp2p-peerstore"
	cid "gx/ipfs/QmR8BauakNcBa3RbE4nbQu76PDiJgoQgz8AJdhJuiU4TAw/go-cid"
	routing "gx/ipfs/QmTiRqrF5zkdZyrdsL5qndG1UbeWi8k8N2pYxCtXWrahR2/go-libp2p-routing"
	pubsub "gx/ipfs/QmVRxA4J3UPQpw74dLrQ6NJkfysCA1H4GU28gVpXQt9zMU/go-libp2p-pubsub"
	peer "gx/ipfs/QmY5Grm8pJdiSSVsYxx4uNRgweY72EmYwuSDbRnbFok3iY/go-libp2p-peer"
	p2phost "gx/ipfs/QmaoXrM4Z41PD48JY36YqQGKQpLGjyLA2cKcLsES7YddAq/go-libp2p-host"
)

type PubSubAPI CoreAPI

type pubSubSubscription struct {
	cancel       context.CancelFunc
	subscription *pubsub.Subscription
}

type pubSubMessage struct {
	msg *pubsub.Message
}

func (api *PubSubAPI) Ls(ctx context.Context) ([]string, error) {
	_, err := api.checkNode()
	if err != nil {
		return nil, err
	}

	return api.pubSub.GetTopics(), nil
}

func (api *PubSubAPI) Peers(ctx context.Context, opts ...caopts.PubSubPeersOption) ([]peer.ID, error) {
	_, err := api.checkNode()
	if err != nil {
		return nil, err
	}

	settings, err := caopts.PubSubPeersOptions(opts...)
	if err != nil {
		return nil, err
	}

	peers := api.pubSub.ListPeers(settings.Topic)
	out := make([]peer.ID, len(peers))

	for i, peer := range peers {
		out[i] = peer
	}

	return out, nil
}

func (api *PubSubAPI) Publish(ctx context.Context, topic string, data []byte) error {
	_, err := api.checkNode()
	if err != nil {
		return err
	}

	return api.pubSub.Publish(topic, data)
}

func (api *PubSubAPI) Subscribe(ctx context.Context, topic string, opts ...caopts.PubSubSubscribeOption) (coreiface.PubSubSubscription, error) {
	options, err := caopts.PubSubSubscribeOptions(opts...)

	r, err := api.checkNode()
	if err != nil {
		return nil, err
	}

	sub, err := api.pubSub.Subscribe(topic)
	if err != nil {
		return nil, err
	}

	pubctx, cancel := context.WithCancel(api.nctx)

	if options.Discover {
		go func() {
			blk, err := api.core().Block().Put(pubctx, strings.NewReader("floodsub:"+topic))
			if err != nil {
				log.Error("pubsub discovery: ", err)
				return
			}

			connectToPubSubPeers(pubctx, r, api.peerHost, blk.Path().Cid())
		}()
	}

	return &pubSubSubscription{cancel, sub}, nil
}

func connectToPubSubPeers(ctx context.Context, r routing.IpfsRouting, ph p2phost.Host, cid cid.Cid) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	provs := r.FindProvidersAsync(ctx, cid, 10)
	var wg sync.WaitGroup
	for p := range provs {
		wg.Add(1)
		go func(pi pstore.PeerInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			err := ph.Connect(ctx, pi)
			if err != nil {
				log.Info("pubsub discover: ", err)
				return
			}
			log.Info("connected to pubsub peer:", pi.ID)
		}(p)
	}

	wg.Wait()
}

func (api *PubSubAPI) checkNode() (routing.IpfsRouting, error) {
	if api.pubSub == nil {
		return nil, errors.New("experimental pubsub feature not enabled. Run daemon with --enable-pubsub-experiment to use.")
	}

	err := api.checkOnline(false)
	if err != nil {
		return nil, err
	}

	return api.routing, nil
}

func (sub *pubSubSubscription) Close() error {
	sub.cancel()
	sub.subscription.Cancel()
	return nil
}

func (sub *pubSubSubscription) Next(ctx context.Context) (coreiface.PubSubMessage, error) {
	msg, err := sub.subscription.Next(ctx)
	if err != nil {
		return nil, err
	}

	return &pubSubMessage{msg}, nil
}

func (msg *pubSubMessage) From() peer.ID {
	return peer.ID(msg.msg.From)
}

func (msg *pubSubMessage) Data() []byte {
	return msg.msg.Data
}

func (msg *pubSubMessage) Seq() []byte {
	return msg.msg.Seqno
}

func (msg *pubSubMessage) Topics() []string {
	return msg.msg.TopicIDs
}

func (api *PubSubAPI) core() coreiface.CoreAPI {
	return (*CoreAPI)(api)
}
