"""Public API for rules_vmtest."""

load("//vmtest:config.bzl", _vmtest_config = "vmtest_config")
load("//vmtest:vm_go_test.bzl", _vm_go_test = "vm_go_test")
load("//vmtest:vm_test.bzl", _vm_test = "vm_test")

vm_test = _vm_test
vm_go_test = _vm_go_test
vmtest_config = _vmtest_config
