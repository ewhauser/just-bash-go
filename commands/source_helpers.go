package commands

import (
	"context"
	"path"
)

type namedInput struct {
	Name      string
	Abs       string
	Data      []byte
	FromStdin bool
}

func readNamedInputs(ctx context.Context, inv *Invocation, names []string, defaultStdin bool) ([]namedInput, error) {
	if len(names) == 0 {
		if !defaultStdin {
			return nil, nil
		}
		data, err := readAllStdin(inv)
		if err != nil {
			return nil, err
		}
		return []namedInput{{
			Name:      "-",
			Abs:       "-",
			Data:      data,
			FromStdin: true,
		}}, nil
	}

	var (
		inputs      []namedInput
		stdinData   []byte
		stdinLoaded bool
	)
	for _, name := range names {
		if name == "-" {
			if !stdinLoaded {
				data, err := readAllStdin(inv)
				if err != nil {
					return nil, err
				}
				stdinData = data
				stdinLoaded = true
			}
			inputs = append(inputs, namedInput{
				Name:      "-",
				Abs:       "-",
				Data:      stdinData,
				FromStdin: true,
			})
			continue
		}
		data, abs, err := readAllFile(ctx, inv, name)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, namedInput{
			Name: path.Base(abs),
			Abs:  abs,
			Data: data,
		})
	}
	return inputs, nil
}
