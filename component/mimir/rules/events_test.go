package rules

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	mimirClient "github.com/grafana/agent/pkg/mimir/client"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promListers "github.com/prometheus-operator/prometheus-operator/pkg/client/listers/monitoring/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	coreListers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type fakeMimirClient struct {
	rules map[string][]mimirClient.RuleGroup
}

var _ mimirClient.Interface = &fakeMimirClient{}

func newFakeMimirClient() *fakeMimirClient {
	return &fakeMimirClient{
		rules: make(map[string][]mimirClient.RuleGroup),
	}
}

func (m *fakeMimirClient) CreateRuleGroup(ctx context.Context, namespace string, rule mimirClient.RuleGroup) error {
	m.DeleteRuleGroup(ctx, namespace, rule.Name)
	m.rules[namespace] = append(m.rules[namespace], rule)
	return nil
}

func (m *fakeMimirClient) DeleteRuleGroup(ctx context.Context, namespace, group string) error {
	for ns, v := range m.rules {
		for i, g := range v {
			if g.Name == group {
				m.rules[ns] = append(m.rules[ns][:i], m.rules[ns][i+1:]...)

				if len(m.rules[ns]) == 0 {
					delete(m.rules, ns)
				}

				return nil
			}
		}
	}
	return nil
}

func (m *fakeMimirClient) ListRules(ctx context.Context, namespace string) (map[string][]mimirClient.RuleGroup, error) {
	output := make(map[string][]mimirClient.RuleGroup)
	for ns, v := range m.rules {
		if namespace != "" && namespace != ns {
			continue
		}
		output[ns] = v
	}
	return output, nil
}

func TestEventLoop(t *testing.T) {
	nsIndexer := cache.NewIndexer(
		cache.DeletionHandlingMetaNamespaceKeyFunc,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	nsLister := coreListers.NewNamespaceLister(nsIndexer)

	ruleIndexer := cache.NewIndexer(
		cache.DeletionHandlingMetaNamespaceKeyFunc,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	ruleLister := promListers.NewPrometheusRuleLister(ruleIndexer)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "namespace",
			UID:  types.UID("33f8860c-bd06-4c0d-a0b1-a114d6b9937b"),
		},
	}

	rule := &v1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			UID:       types.UID("64aab764-c95e-4ee9-a932-cd63ba57e6cf"),
		},
		Spec: v1.PrometheusRuleSpec{
			Groups: []v1.RuleGroup{
				{
					Name: "group",
					Rules: []v1.Rule{
						{
							Alert: "alert",
							Expr:  intstr.FromString("expr"),
						},
					},
				},
			},
		},
	}

	handler := Component{
		log:               log.NewLogfmtLogger(os.Stdout),
		queue:             workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		namespaceLister:   nsLister,
		namespaceSelector: labels.Everything(),
		ruleLister:        ruleLister,
		ruleSelector:      labels.Everything(),
		mimirClient:       newFakeMimirClient(),
		args:              Arguments{MimirNameSpacePrefix: "agent"},
		metrics:           newMetrics(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handler.eventLoop(ctx)

	// Add a namespace and rule to kubernetes
	nsIndexer.Add(ns)
	ruleIndexer.Add(rule)
	handler.OnAdd(rule)

	// Wait for the rule to be added to mimir
	require.Eventually(t, func() bool {
		return len(handler.currentState) == 1
	}, time.Second, 10*time.Millisecond)
	handler.queue.AddRateLimited(event{typ: eventTypeSyncMimir})

	// Update the rule in kubernetes
	rule.Spec.Groups[0].Rules = append(rule.Spec.Groups[0].Rules, v1.Rule{
		Alert: "alert2",
		Expr:  intstr.FromString("expr2"),
	})
	ruleIndexer.Update(rule)
	handler.OnUpdate(rule, rule)

	// Wait for the rule to be updated in mimir
	require.Eventually(t, func() bool {
		rules := handler.currentState[mimirNamespaceForRuleCRD("agent", rule)][0].Rules
		return len(rules) == 2
	}, time.Second, 10*time.Millisecond)
	handler.queue.AddRateLimited(event{typ: eventTypeSyncMimir})

	// Remove the rule from kubernetes
	ruleIndexer.Delete(rule)
	handler.OnDelete(rule)

	// Wait for the rule to be removed from mimir
	require.Eventually(t, func() bool {
		return len(handler.currentState) == 0
	}, time.Second, 10*time.Millisecond)
}
