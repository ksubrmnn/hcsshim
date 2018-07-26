package main

import (
	"github.com/Microsoft/hcsshim/internal/appargs"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

var syncNetNsCommand = cli.Command{
	Name:  "sync-netns",
	Usage: "sync-netns attempts to synchronize the state of the network namespace endpoints associated with the container",
	ArgsUsage: `<container-id>

Where "<container-id>" is the name for the instance of the container. This is
usually a the id of the 'PodSandbox' but could be any container with a network namespace. 

EXAMPLE:
For example, if the container id is "pod-01" the following will synchronize the list of endpoints associated
with the containers network namespace:

	   # runhcs sync-netns pod-01
	   
ERRORS:
If "<container-id>" does not exist error returned: "ID '<container-id>' was not found"
If "<container-id>" is not running error returned: "ID '<container-id>' is not running"`,
	Flags:  []cli.Flag{},
	Before: appargs.Validate(argID, appargs.Optional(appargs.String)),
	Action: func(context *cli.Context) error {
		id := context.Args().First()
		c, err := getContainer(id, true)
		if err != nil {
			return err
		}
		status, err := c.Status()
		if err != nil {
			return err
		}
		if status != containerCreated &&
			status != containerRunning &&
			status != containerPaused {
			return errors.Errorf("ID '%s' is not running", id)
		}

		var pid int
		if err := stateKey.Get(id, keyInitPid, &pid); err != nil {
			return err
		}

		p, err := c.hc.OpenProcess(pid)
		if err != nil {
			return err
		}
		defer p.Close()
		// TODO: JUTERRY p.SyncNetNs()
		return errors.New("not implemented")
	},
}
