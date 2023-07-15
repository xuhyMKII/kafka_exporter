package tool

import (
	"flag"
	"github.com/alecthomas/kingpin/v2"
)

func ToFlagString(name string, help string, value string) *string {
	flag.CommandLine.String(name, value, help) // hack around flag.Parse and klog.init flags
	return kingpin.Flag(name, help).Default(value).String()
}

func ToFlagBool(name string, help string, value bool, valueString string) *bool {
	flag.CommandLine.Bool(name, value, help) // hack around flag.Parse and klog.init flags
	return kingpin.Flag(name, help).Default(valueString).Bool()
}

func ToFlagStringsVar(name string, help string, value string, target *[]string) {
	flag.CommandLine.String(name, value, help) // hack around flag.Parse and klog.init flags
	kingpin.Flag(name, help).Default(value).StringsVar(target)
}

func ToFlagStringVar(name string, help string, value string, target *string) {
	flag.CommandLine.String(name, value, help) // hack around flag.Parse and klog.init flags
	kingpin.Flag(name, help).Default(value).StringVar(target)
}

func ToFlagBoolVar(name string, help string, value bool, valueString string, target *bool) {
	flag.CommandLine.Bool(name, value, help) // hack around flag.Parse and klog.init flags
	kingpin.Flag(name, help).Default(valueString).BoolVar(target)
}

func ToFlagIntVar(name string, help string, value int, valueString string, target *int) {
	flag.CommandLine.Int(name, value, help) // hack around flag.Parse and klog.init flags
	kingpin.Flag(name, help).Default(valueString).IntVar(target)
}
