package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/lcow"
	hcsschema "github.com/Microsoft/hcsshim/internal/schema2"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	kernelArgsArgName           = "kernel-args"
	rootFSTypeArgName           = "root-fs-type"
	vpMemMaxCountArgName        = "vpmem-max-count"
	vpMemMaxSizeArgName         = "vpmem-max-size"
	cpusArgName                 = "cpus"
	memoryArgName               = "memory"
	allowOvercommitArgName      = "allow-overcommit"
	enableDeferredCommitArgName = "enable-deferred-commit"
	measureArgName              = "measure"
	parallelArgName             = "parallel"
	countArgName                = "count"
	kernelDirectArgName         = "kernel-direct"
	execCommandLineArgName      = "exec"
	forwardStdoutArgName        = "fwd-stdout"
	forwardStderrArgName        = "fwd-stderr"
	debugArgName                = "debug"
	outputHandlingArgName       = "output-handling"
	consolePipeArgName          = "console-pipe"
	gcsArgName                  = "gcs"
)

func main() {
	app := cli.NewApp()
	app.Name = "uvmboot"
	app.Usage = "Boot a utility VM"

	app.Flags = []cli.Flag{
		cli.Uint64Flag{
			Name:  cpusArgName,
			Usage: "Number of CPUs on the UVM. Uses hcsshim default if not specified",
		},
		cli.UintFlag{
			Name:  memoryArgName,
			Usage: "Amount of memory on the UVM, in MB. Uses hcsshim default if not specified",
		},
		cli.BoolFlag{
			Name:  measureArgName,
			Usage: "Measure wall clock time of the UVM run",
		},
		cli.IntFlag{
			Name:  parallelArgName,
			Value: 1,
			Usage: "Number of UVMs to boot in parallel",
		},
		cli.IntFlag{
			Name:  countArgName,
			Value: 1,
			Usage: "Total number of UVMs to run",
		},
		cli.BoolFlag{
			Name:  allowOvercommitArgName,
			Usage: "Allow memory overcommit on the UVM",
		},
		cli.BoolFlag{
			Name:  enableDeferredCommitArgName,
			Usage: "Enable deferred commit on the UVM",
		},
		cli.BoolFlag{
			Name:  debugArgName,
			Usage: "Enable debug level logging in HCSShim",
		},
		cli.BoolFlag{
			Name:  gcsArgName,
			Usage: "Launch the GCS and perform requested operations via its RPC interface",
		},
	}

	app.Commands = []cli.Command{
		{
			Name:  "lcow",
			Usage: "Boot an LCOW UVM",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  kernelArgsArgName,
					Value: "",
					Usage: "Additional arguments to pass to the kernel",
				},
				cli.StringFlag{
					Name:  rootFSTypeArgName,
					Usage: "Either 'initrd' or 'vhd'. (default: 'vhd' if rootfs.vhd exists)",
				},
				cli.UintFlag{
					Name:  vpMemMaxCountArgName,
					Usage: "Number of VPMem devices on the UVM. Uses hcsshim default if not specified",
				},
				cli.Uint64Flag{
					Name:  vpMemMaxSizeArgName,
					Usage: "Size of each VPMem device, in MB. Uses hcsshim default if not specified",
				},
				cli.BoolFlag{
					Name:  kernelDirectArgName,
					Usage: "Use kernel direct booting for UVM (default: true on builds >= 18286)",
				},
				cli.StringFlag{
					Name:  execCommandLineArgName,
					Usage: "Command to execute in the UVM.",
				},
				cli.BoolFlag{
					Name:  forwardStdoutArgName,
					Usage: "Whether stdout from the process in the UVM should be forwarded",
				},
				cli.BoolFlag{
					Name:  forwardStderrArgName,
					Usage: "Whether stderr from the process in the UVM should be forwarded",
				},
				cli.StringFlag{
					Name:  outputHandlingArgName,
					Usage: "Controls how output from UVM is handled. Use 'stdout' to print all output to stdout",
				},
				cli.StringFlag{
					Name:  consolePipeArgName,
					Usage: "Named pipe for serial console output (which will be enabled)",
				},
			},
			Action: func(c *cli.Context) error {
				if c.GlobalBool("debug") {
					logrus.SetLevel(logrus.DebugLevel)
				} else {
					logrus.SetLevel(logrus.WarnLevel)
				}

				parallelCount := c.GlobalInt(parallelArgName)

				var wg sync.WaitGroup
				wg.Add(parallelCount)

				workChan := make(chan int)

				runFunc := func(workChan <-chan int) {
					for {
						i, ok := <-workChan

						if !ok {
							wg.Done()
							return
						}

						id := fmt.Sprintf("uvmboot-%d", i)

						options := uvm.NewDefaultOptionsLCOW(id, "")
						useGcs := c.GlobalBool(gcsArgName)
						options.UseGuestConnection = useGcs

						if c.GlobalIsSet(cpusArgName) {
							options.ProcessorCount = int32(c.GlobalUint64(cpusArgName))
						}
						if c.GlobalIsSet(memoryArgName) {
							options.MemorySizeInMB = int32(c.GlobalUint64(memoryArgName))
						}
						if c.GlobalIsSet(allowOvercommitArgName) {
							options.AllowOvercommit = c.GlobalBool(allowOvercommitArgName)
						}
						if c.GlobalIsSet(enableDeferredCommitArgName) {
							options.EnableDeferredCommit = c.GlobalBool(enableDeferredCommitArgName)
						}

						if c.IsSet(kernelDirectArgName) {
							options.KernelDirect = c.Bool(kernelDirectArgName)
						}
						if c.IsSet(rootFSTypeArgName) {
							switch strings.ToLower(c.String(rootFSTypeArgName)) {
							case "initrd":
								options.RootFSFile = uvm.InitrdFile
								options.PreferredRootFSType = uvm.PreferredRootFSTypeInitRd
							case "vhd":
								options.RootFSFile = uvm.VhdFile
								options.PreferredRootFSType = uvm.PreferredRootFSTypeVHD
							default:
								logrus.Fatalf("Unrecognized value '%s' for option %s", c.String(rootFSTypeArgName), rootFSTypeArgName)
							}
						}
						if c.IsSet(kernelArgsArgName) {
							options.KernelBootOptions = c.String(kernelArgsArgName)
						}
						if c.IsSet(vpMemMaxCountArgName) {
							options.VPMemDeviceCount = uint32(c.Uint(vpMemMaxCountArgName))
						}
						if c.IsSet(vpMemMaxSizeArgName) {
							options.VPMemSizeBytes = c.Uint64(vpMemMaxSizeArgName) * 1024 * 1024 // convert from MB to bytes
						}
						if !useGcs {
							if c.IsSet(execCommandLineArgName) {
								options.ExecCommandLine = c.String(execCommandLineArgName)
							}
							if c.IsSet(forwardStdoutArgName) {
								options.ForwardStdout = c.Bool(forwardStdoutArgName)
							}
							if c.IsSet(forwardStderrArgName) {
								options.ForwardStderr = c.Bool(forwardStderrArgName)
							}
							if c.IsSet(outputHandlingArgName) {
								switch strings.ToLower(c.String(outputHandlingArgName)) {
								case "stdout":
									options.OutputHandler = uvm.OutputHandler(func(r io.Reader) { io.Copy(os.Stdout, r) })
								default:
									logrus.Fatalf("Unrecognized value '%s' for option %s", c.String(outputHandlingArgName), outputHandlingArgName)
								}
							}
						}
						if c.IsSet(consolePipeArgName) {
							options.ConsolePipe = c.String(consolePipeArgName)
						}

						if err := run(options, c); err != nil {
							logrus.WithField("uvm-id", id).Error(err)
						}
					}
				}

				for i := 0; i < parallelCount; i++ {
					go runFunc(workChan)
				}

				start := time.Now()

				for i := 0; i < c.GlobalInt(countArgName); i++ {
					workChan <- i
				}

				close(workChan)

				wg.Wait()

				if c.GlobalBool(measureArgName) {
					fmt.Println("Elapsed time:", time.Since(start))
				}

				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		logrus.Fatal(err)
	}
}

func run(options *uvm.OptionsLCOW, c *cli.Context) error {
	uvm, err := uvm.CreateLCOW(options)
	if err != nil {
		return err
	}
	defer uvm.Close()

	if err := uvm.Start(); err != nil {
		return err
	}

	if options.UseGuestConnection {
		if err := execViaGcs(uvm.ComputeSystem(), c); err != nil {
			return err
		}
		uvm.ComputeSystem().Terminate()
		uvm.Wait()
		return uvm.ComputeSystem().ExitError()
	}

	return uvm.Wait()
}

func execViaGcs(cs *hcs.System, c *cli.Context) error {
	var copyOut, copyErr bool
	if c.String(outputHandlingArgName) == "stdout" {
		copyOut = c.Bool(forwardStdoutArgName)
		copyErr = c.Bool(forwardStderrArgName)
	}
	popts := &lcow.ProcessParameters{
		ProcessParameters: hcsschema.ProcessParameters{
			CommandArgs:      []string{"/bin/sh", "-c", c.String(execCommandLineArgName)},
			Environment:      map[string]string{"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			WorkingDirectory: "/",
			CreateStdOutPipe: copyOut,
			CreateStdErrPipe: copyErr,
		},
		CreateInUtilityVm: true,
	}
	p, err := cs.CreateProcess(popts)
	if err != nil {
		return err
	}
	defer p.Close()
	_, pout, perr := p.Stdio()
	ch := make(chan error)
	n := 0
	asyncCopy := func(w io.Writer, r io.Reader, name string) {
		n++
		go func() {
			_, err := io.Copy(w, r)
			if err != nil {
				err = fmt.Errorf("%s: %s", name, err)
			}
			ch <- err
		}()
	}
	if copyOut {
		asyncCopy(os.Stdout, pout, "stdout")
	}
	if copyErr {
		asyncCopy(os.Stdout, perr, "stderr") // match non-GCS behavior and forward to stdout
	}
	if err = p.Wait(); err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		if err := <-ch; err != nil {
			return err
		}
	}
	if err = p.Close(); err != nil {
		return err
	}
	return nil
}
