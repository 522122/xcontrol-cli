package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	cpuControl     = "/sys/devices/system/cpu"
	splControl     = "/sys/devices/platform/asus-nb-wmi/ppt_pl1_spl"
	fpptControl    = "/sys/devices/platform/asus-nb-wmi/ppt_fppt"
	spptControl    = "/sys/devices/platform/asus-nb-wmi/ppt_pl2_sppt"
	profileControl = "/sys/firmware/acpi/platform_profile"
	chargeControl  = "/sys/class/power_supply/BAT0/charge_control_end_threshold"
	// smtControl     = "/sys/devices/system/cpu/smt/control"
	smtActive      = "/sys/devices/system/cpu/smt/active"
	boostControl   = "/sys/devices/system/cpu/cpufreq/boost"
	physCores      = 8
	unsetFlagValue = -1
)

type Json struct {
	Cores  *string `json:"cores"`
	Tdp    *string `json:"tdp"`
	Charge *string `json:"charge"`
	Smt    *string `json:"smt"`
	Boost  *string `json:"boost"`
}

func readSysValue(control string) (string, error) {
	file, err := os.Open(control)
	if err != nil {
		return "", err
	}
	defer file.Close()

	buf := make([]byte, 16)
	n, err := file.Read(buf)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(buf[:n])), nil
}

func setSysValue(control, value string) error {
	file, err := os.OpenFile(control, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.WriteString(value); err != nil {
		return err
	}

	fmt.Println(value, ">", control)
	return nil
}

func simpleValueTransform(n int) string {
	return strconv.Itoa(n)
}

// func smtValueTransform(n int) string {
// 	if n == 1 {
// 		return "on"
// 	} else {
// 		return "off"
// 	}
// }

func tdpValueTransform(n int) string {
	switch {
	case n < 17:
		return "quiet"
	case n < 25:
		return "balanced"
	default:
		return "performance"
	}
}

func cpuValueTransform(n int) func(int) string {
	return func(i int) string {
		if i < n*2-2 {
			return "1"
		} else {
			return "0"
		}
	}
}

func cpuValueVirtTransform(n int, smt string) func(int) string {
	return func(i int) string {
		physCore := cpuValueTransform(n)(i)

		if smt == "off" {
			return "0"
		} else {
			return physCore
		}
	}
}

func simpleSetFunc(control string, valueTransform func(int) string) func(int) error {
	return func(n int) error {
		return setSysValue(control, valueTransform(n))
	}
}

func simpleReadFunc(control string) func() (string, error) {
	return func() (string, error) {
		return readSysValue(control)
	}
}

func readCpuCount() (string, error) {
	coresValue := 1

	for i := 1; i < physCores; i++ {
		if coreValue, err := readSysValue(fmt.Sprintf("%s/cpu%d/online", cpuControl, i)); err == nil {
			if coreValue == "1" {
				coresValue++
			}
		}
	}

	return strconv.Itoa(coresValue), nil
}

func readSMTstatus() (string, error) {
	for i := 1; i < physCores; i++ {
		control1 := fmt.Sprintf("%s/cpu%d/online", cpuControl, i)
		control2 := fmt.Sprintf("%s/cpu%d/online", cpuControl, i+physCores)

		online1, err1 := readSysValue(control1)
		online2, err2 := readSysValue(control2)

		if err1 != nil || err2 != nil {
			return "off", fmt.Errorf("failed to read core online status, %v, %v", err1, err2)
		}

		if online1 != online2 && online1 == "1" {
			return "off", nil
		}
	}

	return "on", nil
}

func setCpuCount(n int) error {
	var tasks []func(int) error
	var errList []error
	smtStatus, _ := readSMTstatus()

	for i := 1; i < physCores; i++ {
		control1 := fmt.Sprintf("%s/cpu%d/online", cpuControl, i)
		control2 := fmt.Sprintf("%s/cpu%d/online", cpuControl, i+physCores)
		tasks = append(tasks,
			simpleSetFunc(control1, cpuValueTransform(n)),
			simpleSetFunc(control2, cpuValueVirtTransform(n, smtStatus)),
		)
	}

	// order is 1,9,2,10,3,11 ...
	for i, task := range tasks {
		if err := task(i); err != nil {
			errList = append(errList, fmt.Errorf("core %d: %v", i, err))
		}
	}

	if len(errList) > 0 {
		return fmt.Errorf("failed to set some CPU cores: %v", errList)
	}
	return nil
}

func setSmt(n int) error {
	var tasks []func(int) error
	var errList []error

	setOffline := func(n int) string {
		return "0"
	}

	for i := 0; i < physCores; i++ {
		control1 := fmt.Sprintf("%s/cpu%d/online", cpuControl, i)
		control2 := fmt.Sprintf("%s/cpu%d/online", cpuControl, i+physCores)

		if i > 0 {
			if isOnline, _ := readSysValue(control1); isOnline == "0" {
				tasks = append(tasks, simpleSetFunc(control2, setOffline))
				continue
			}
		}

		tasks = append(tasks, simpleSetFunc(control2, simpleValueTransform))
	}

	for i, task := range tasks {
		if err := task(n); err != nil {
			errList = append(errList, fmt.Errorf("SMT core %d: %v", i, err))
		}
	}

	if len(errList) > 0 {
		return fmt.Errorf("failed to set some CPU cores: %v", errList)
	}
	return nil
}

func setTdp(n int) error {

	tasks := [4]func(int) error{
		simpleSetFunc(profileControl, tdpValueTransform),
		simpleSetFunc(splControl, simpleValueTransform),
		simpleSetFunc(fpptControl, simpleValueTransform),
		simpleSetFunc(spptControl, simpleValueTransform),
	}

	for _, task := range tasks {
		if err := task(n); err != nil {
			return err
		}
	}

	return nil
}

func validateNumber(n int, min int, max int) error {
	if n < min || n > max {
		return fmt.Errorf("%d is out of range (%d - %d)", n, min, max)
	}
	return nil
}

func inRange(min int, max int) func(int) error {
	return func(n int) error {
		return validateNumber(n, min, max)
	}
}

func validateAndApply(n int, validate func(int) error, flagName string, setFunc func(int) error) {
	if n == unsetFlagValue {
		return
	}

	if err := validate(n); err != nil {
		fmt.Printf("Input error: %s %s\n", flagName, err)
		return
	}

	if err := setFunc(n); err != nil {
		fmt.Printf("Error setting %s to value %d: %s\n", flagName, n, err)
	}
}

func readAndAssign(k **string, readFn func() (string, error)) {
	if value, err := readFn(); err == nil {
		*k = &value
	}
}

func main() {
	var (
		boost    = flag.Int("boost", unsetFlagValue, "Control CPU boost (0 | 1)")
		charge   = flag.Int("charge", unsetFlagValue, "Control max battery charge limit (50 - 100)")
		cores    = flag.Int("cores", unsetFlagValue, "Control online CPU cores (2 - 8)")
		smt      = flag.Int("smt", unsetFlagValue, "Control Simultaneous Multi-Threading (0 | 1)")
		tdp      = flag.Int("tdp", unsetFlagValue, "Control TDP limit (8 - 25)")
		jsonFlag = flag.Bool("json", false, "Output in JSON format")
	)

	flag.Parse()

	if flag.NFlag() == 0 {
		fmt.Println("Usage:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *jsonFlag {

		var jsonData Json

		readAndAssign(&jsonData.Boost, simpleReadFunc(boostControl))
		readAndAssign(&jsonData.Charge, simpleReadFunc(chargeControl))
		readAndAssign(&jsonData.Smt, readSMTstatus)
		readAndAssign(&jsonData.Tdp, simpleReadFunc(splControl))
		readAndAssign(&jsonData.Cores, readCpuCount)

		if jsonBytes, err := json.MarshalIndent(jsonData, "", " "); err == nil {
			fmt.Println(string(jsonBytes))
		}

		os.Exit(0)
	}

	validateAndApply(*cores, inRange(2, physCores), "-cores", setCpuCount)
	validateAndApply(*tdp, inRange(8, 25), "-tdp", setTdp)
	validateAndApply(*charge, inRange(50, 100), "-charge", simpleSetFunc(chargeControl, simpleValueTransform))
	validateAndApply(*smt, inRange(0, 1), "-smt", setSmt)
	validateAndApply(*boost, inRange(0, 1), "-boost", simpleSetFunc(boostControl, simpleValueTransform))
}
