package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func main() {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	allocate := kernel.NewProc("AllocConsole")
	interrupt := kernel.NewProc("GenerateConsoleCtrlEvent")

	allocate.Call()

	binaries := []string{"measurements/anchored-original-cli.exe", "measurements/anchored-scalar-cli.exe"}
	names := []string{"before", "after"}

	for pair := range 4 {
		order := []int{0, 1}

		if pair%2 != 0 {
			order[0], order[1] = order[1], order[0]
		}

		for _, index := range order {
			name := fmt.Sprintf("measurements/anchored-cli-%d-%s", pair, names[index])

			output, err := os.Create(name + ".txt")
			if err != nil {
				panic(err)
			}

			command := exec.Command(binaries[index], "--cpu", "32", "--output", name+"-matches", "donate.", "mirror.", "secure.")

			command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
			command.Stdout = output
			command.Stderr = output

			err = command.Start()
			if err != nil {
				panic(err)
			}

			time.Sleep(10 * time.Second)

			// CTRL_BREAK targets only the child's process group; Go handles it as os.Interrupt.
			result, _, signalError := interrupt.Call(1, uintptr(command.Process.Pid))
			if result == 0 {
				command.Process.Kill()
				panic(signalError)
			}

			err = command.Wait()
			output.Close()

			if err != nil {
				panic(err)
			}

			fmt.Println(name)
		}
	}
}
