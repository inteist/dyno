package cli

import "flag"

// ModelsCommand lists the local models. Only Common.Host is set.
type ModelsCommand struct{ Common }

func (ModelsCommand) command() {}

// defineModels registers the flags of the models command.
func defineModels(fs *flag.FlagSet, env Env) builder {
	cmd := &ModelsCommand{}
	cmd.addProvider(fs)
	return func() (Command, error) {
		if err := cmd.resolveProvider(fs, env); err != nil {
			return nil, err
		}
		return *cmd, nil
	}
}
