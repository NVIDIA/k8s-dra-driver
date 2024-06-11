package main

import (
	"context"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	"time"
)

func buildLeaderElectionConfig(config *Config, name, nameSpace string, run func(context.Context, *Config) error) leaderelection.LeaderElectionConfig {
	id := uuid.New().String()
	LeaderElectionLock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: nameSpace,
		},
		Client: config.clientSets.Core.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}
	cfg := leaderelection.LeaderElectionConfig{
		Lock:            LeaderElectionLock,
		ReleaseOnCancel: false,
		LeaseDuration:   60 * time.Second,
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				err := run(ctx, config)
				if err != nil {
					klog.Fatalf("error running leader: %v", err)
				}
			},
			OnStoppedLeading: func() {
				klog.Infof("leader lost: %s", id)
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	}
	return cfg
}
