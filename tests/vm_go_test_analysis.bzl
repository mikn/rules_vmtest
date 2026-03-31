"""Analysis tests for the vm_go_test Starlark rule (using rules_testing)."""

load("@rules_testing//lib:analysis_test.bzl", "analysis_test")

def _get_write_action_content(env, target):
    """Return the content of the first FileWrite action on the target."""
    actions = target.actions
    write_actions = [a for a in actions if a.mnemonic == "FileWrite"]
    env.expect.that_int(len(write_actions)).is_greater_than(0)
    if not write_actions:
        return ""
    return write_actions[0].content

# --- Test: vm_go_test generates script with correct env vars ---

def _basic_env_vars_test_impl(env, target):
    # The rule should produce an executable
    files_to_run = target[DefaultInfo].files_to_run
    env.expect.that_bool(files_to_run != None).equals(True)
    env.expect.that_bool(files_to_run.executable != None).equals(True)
    env.expect.that_str(files_to_run.executable.basename).contains(".sh")

    # Check the generated script content via actions
    script = _get_write_action_content(env, target)
    env.expect.that_str(script).contains("VMTEST_MEMORY")
    env.expect.that_str(script).contains('"4G"')
    env.expect.that_str(script).contains("VMTEST_CPUS")
    env.expect.that_str(script).contains('"4"')
    env.expect.that_str(script).contains("VMTEST_NETWORK")

# --- Test: vm_go_test with bridge networking sets bridge env vars ---

def _bridge_network_test_impl(env, target):
    script = _get_write_action_content(env, target)
    env.expect.that_str(script).contains('VMTEST_NETWORK="bridge"')
    env.expect.that_str(script).contains('VMTEST_BRIDGE="test-br0"')

# --- Test: vm_go_test with TPM sets TPM env vars ---

def _tpm_env_vars_test_impl(env, target):
    script = _get_write_action_content(env, target)
    env.expect.that_str(script).contains('VMTEST_TPM="true"')
    env.expect.that_str(script).contains("VMTEST_SWTPM")
    env.expect.that_str(script).contains("VMTEST_SWTPM_SETUP")

# --- Test: vm_go_test with port_forwards sets VMTEST_PORT_FORWARDS ---

def _port_forwards_test_impl(env, target):
    script = _get_write_action_content(env, target)
    env.expect.that_str(script).contains("VMTEST_PORT_FORWARDS")
    env.expect.that_str(script).contains("50051,50052")

# --- Test: vm_go_test includes test binary in runfiles ---

def _runfiles_test_impl(env, target):
    runfiles = target[DefaultInfo].default_runfiles.files.to_list()
    basenames = [f.basename for f in runfiles]
    has_test_binary = "mock_test" in basenames or "mock_test.sh" in basenames

    # Verify the test binary (or its wrapper) appears in runfiles.
    env.expect.that_bool(has_test_binary).equals(True)

def vm_go_test_analysis_test_suite(name):
    """Define the test suite for vm_go_test analysis tests.

    Args:
        name: The name of the test suite target.
    """
    analysis_test(
        name = "basic_env_vars_test",
        impl = _basic_env_vars_test_impl,
        target = "//tests:basic_subject",
    )

    analysis_test(
        name = "bridge_network_test",
        impl = _bridge_network_test_impl,
        target = "//tests:bridge_subject",
    )

    analysis_test(
        name = "tpm_env_vars_test",
        impl = _tpm_env_vars_test_impl,
        target = "//tests:tpm_subject",
    )

    analysis_test(
        name = "port_forwards_test",
        impl = _port_forwards_test_impl,
        target = "//tests:port_forwards_subject",
    )

    analysis_test(
        name = "runfiles_test",
        impl = _runfiles_test_impl,
        target = "//tests:basic_subject",
    )

    native.test_suite(
        name = name,
        tests = [
            ":basic_env_vars_test",
            ":bridge_network_test",
            ":tpm_env_vars_test",
            ":port_forwards_test",
            ":runfiles_test",
        ],
    )
