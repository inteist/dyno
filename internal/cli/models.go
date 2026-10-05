package cli

import "flag"

// ModelsCommand lists the local models. Only Common.Host is set.
type ModelsCommand struct{ Common }

func (ModelsCommand) command() {}

// defineModels registers the flags of the models command.
func defineModels(fs *flag.FlagSet, _ Env) builder {
	cmd := &ModelsCommand{}
	cmd.addHost(fs)
	return func() (Command, error) { return *cmd, nil }
}
