package client

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/go-kratos/kratos/v2/selector"
	"github.com/go-kratos/kratos/v2/selector/p2c"
	"strings"
	"time"

	config "github.com/kwseeker/kratos-gateway/gateway-02/api/gateway/config/v1"
)

// Factory is returns service client.
type Factory func(*config.Endpoint) (Client, error)

// NewFactory new a client factory.
func NewFactory(logger log.Logger, r registry.Discovery) Factory {
	l := log.NewHelper(logger)
	return func(endpoint *config.Endpoint) (Client, error) {
		//创建p2c选择器（两次随机选择）
		picker := p2c.New()
		applier := &nodeApplier{
			endpoint:  endpoint,
			logHelper: l,
			registry:  r,
		}
		//应用负载均衡算法，从服务节点列表中选择一个节点
		if err := applier.apply(context.Background(), picker); err != nil {
			return nil, err
		}
		//使用选中的服务节点创建连接
		client := &client{
			selector: picker,
			attempts: calcAttempts(endpoint),
			protocol: endpoint.Protocol,
		}
		retryCond, err := parseRetryCondition(endpoint)
		if err != nil {
			return nil, err
		}
		client.conditions = retryCond
		log.Debug("create actual request client ...")
		return client, nil
	}
}

type nodeApplier struct {
	endpoint  *config.Endpoint
	logHelper *log.Helper
	//节点列表
	registry registry.Discovery
}

/*
 */
func (na *nodeApplier) apply(ctx context.Context, dst selector.Selector) error {
	var nodes []selector.Node
	for _, backend := range na.endpoint.Backends {
		log.Debugf("解析Endpoint, 创建微服务客户端连接：%v", backend)
		target, err := parseTarget(backend.Target)
		if err != nil {
			return err
		}
		weighted := backend.Weight
		switch target.Scheme {
		case "direct":
			node := newNode(backend.Target, na.endpoint.Protocol, weighted, calcTimeout(na.endpoint))
			nodes = append(nodes, node)
			dst.Apply(nodes)
		case "discovery":
			w, err := na.registry.Watch(ctx, target.Endpoint)
			if err != nil {
				return err
			}
			go func() {
				// TODO: goroutine leak
				// only one backend configuration allowed when using service discovery
				for {
					services, err := w.Next()
					if err != nil && errors.Is(err, context.Canceled) {
						return
					}
					if len(services) == 0 {
						continue
					}
					var nodes []selector.Node
					for _, ser := range services {
						scheme := strings.ToLower(na.endpoint.Protocol.String())
						addr, err := parseEndpoint(ser.Endpoints, scheme, false)
						if err != nil {
							na.logHelper.Errorf("failed to parse endpoint: %v", err)
							continue
						}
						node := newNode(addr, na.endpoint.Protocol, weighted, calcTimeout(na.endpoint))
						nodes = append(nodes, node)
					}
					dst.Apply(nodes)
				}
			}()
		default:
			return fmt.Errorf("unknown scheme: %s", target.Scheme)
		}
	}
	return nil
}

func calcTimeout(endpoint *config.Endpoint) time.Duration {
	timeout := endpoint.Timeout.AsDuration()
	if endpoint.Retry == nil {
		return timeout
	}
	if endpoint.Retry.PerTryTimeout != nil &&
		endpoint.Retry.PerTryTimeout.AsDuration() > 0 &&
		endpoint.Retry.PerTryTimeout.AsDuration() < timeout {
		return endpoint.Retry.PerTryTimeout.AsDuration()
	}
	return timeout
}

func calcAttempts(endpoint *config.Endpoint) uint32 {
	if endpoint.Retry == nil {
		return 1
	}
	if endpoint.Retry.Attempts == 0 {
		return 1
	}
	return endpoint.Retry.Attempts
}

func parseRetryCondition(endpoint *config.Endpoint) ([]retryCondition, error) {
	//if endpoint.Retry == nil {
	//	return []retryCondition{}, nil
	//}
	//
	//conditions := make([]retryCondition, 0, len(endpoint.Retry.Conditions))
	//for _, rawCond := range endpoint.Retry.Conditions {
	//	switch v := rawCond.Condition.(type) {
	//	case *config.RetryCondition_ByHeader:
	//		cond := &byHeader{
	//			RetryCondition_ByHeader: v,
	//		}
	//		if err := cond.prepare(); err != nil {
	//			return nil, err
	//		}
	//		conditions = append(conditions, cond)
	//	case *config.RetryCondition_ByStatusCode:
	//		cond := &byStatusCode{
	//			RetryCondition_ByStatusCode: v,
	//		}
	//		if err := cond.prepare(); err != nil {
	//			return nil, err
	//		}
	//		conditions = append(conditions, cond)
	//	default:
	//		return nil, fmt.Errorf("unknown condition type: %T", v)
	//	}
	//}
	//return conditions, nil
	return []retryCondition{}, nil
}
