// Package attributes provides an otelcol.processor.attributes component.
package attributes

import (
	"github.com/grafana/agent/component"
	"github.com/grafana/agent/component/otelcol"
	"github.com/grafana/agent/component/otelcol/processor"
	"github.com/grafana/agent/pkg/river"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/attributesprocessor"
	otelcomponent "go.opentelemetry.io/collector/component"
	otelconfig "go.opentelemetry.io/collector/config"
)

func init() {
	component.Register(component.Registration{
		Name:    "otelcol.processor.attributes",
		Args:    Arguments{},
		Exports: otelcol.ConsumerExports{},

		Build: func(opts component.Options, args component.Arguments) (component.Component, error) {
			fact := attributesprocessor.NewFactory()
			return processor.New(opts, fact, args.(Arguments))
		},
	})
}

// Arguments configures the otelcol.processor.attributes component.
type Arguments struct {
	Key    string `river:"key,attr"`
	Action string `river:"action,attr"`

	Value         string `river:"value,attr,optional"`
	RegexPattern  string `river:"pattern,optional"`
	FromAttribute string `river:"from_attribute,optional"`
	FromContext   string `river:"from_context,optional"`
	ConvertedType string `river:"converted_type,optional"`

	MatchConfig otelcol.MatchConfig `river:"match_config,optional"`

	// Output configures where to send processed data. Required.
	Output *otelcol.ConsumerArguments `river:"output,block"`
}

var (
	_ processor.Arguments = Arguments{}
	_ river.Unmarshaler   = (*Arguments)(nil)
)

// DefaultArguments holds default settings for Arguments.
// TODO: There should either be no default, or the default config should not change any data
// var DefaultArguments = Arguments{
// 	Timeout:       200 * time.Millisecond,
// 	SendBatchSize: 8192,
// }

// UnmarshalRiver implements river.Unmarshaler. It applies defaults to args and
// validates settings provided by the user.
func (args *Arguments) UnmarshalRiver(f func(interface{}) error) error {
	//TODO: Should we have default arguments?
	// *args = DefaultArguments
	*args = Arguments{}

	//TODO: What does this do? Type checking, like a dynamic cast?
	type arguments Arguments
	if err := f((*arguments)(args)); err != nil {
		return err
	}

	//TODO: Doesn't otel validate these kinds of things?
	// if args.SendBatchMaxSize > 0 && args.SendBatchMaxSize < args.SendBatchSize {
	// 	return fmt.Errorf("send_batch_max_size must be greater or equal to send_batch_size when not 0")
	// }
	return nil
}

// Convert implements processor.Arguments.
func (args Arguments) Convert() otelconfig.Processor {
	return &attributesprocessor.Config{
		ProcessorSettings: otelconfig.NewProcessorSettings(otelconfig.NewComponentID("attributes")),
		MatchConfig:       *()(&args.MatchConfig).Convert(),
		Settings:          args.SendBatchSize,
	}
}

// Extensions implements processor.Arguments.
func (args Arguments) Extensions() map[otelconfig.ComponentID]otelcomponent.Extension {
	return nil
}

// Exporters implements processor.Arguments.
func (args Arguments) Exporters() map[otelconfig.DataType]map[otelconfig.ComponentID]otelcomponent.Exporter {
	return nil
}

// NextConsumers implements processor.Arguments.
func (args Arguments) NextConsumers() *otelcol.ConsumerArguments {
	return args.Output
}
