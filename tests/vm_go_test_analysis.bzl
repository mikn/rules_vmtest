"""Analysis tests for the vm_go_test Starlark rule."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")

# --- Test: vm_go_test generates script with correct env vars ---

def _basic_env_vars_test_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    # The rule should produce an executable
    files_to_run = target[DefaultInfo].files_to_run
    asserts.true(env, files_to_run != None, "should have files_to_run")
    asserts.true(env, files_to_run.executable != None, "should have an executable")
    asserts.true(env, files_to_run.executable.basename.endswith(".sh"), "executable should be a shell script")

    # Check the generated script content via actions
    actions = analysistest.target_actions(env)
    write_actions = [a for a in actions if a.content != None]
    asserts.true(env, len(write_actions) > 0, "should have at least one write action")

    script = write_actions[0].content
    asserts.true(env, "VMTEST_MEMORY" in script, "script should set VMTEST_MEMORY")
    asserts.true(env, '"4G"' in script, "VMTEST_MEMORY should be 4G")
    asserts.true(env, "VMTEST_CPUS" in script, "script should set VMTEST_CPUS")
    asserts.true(env, '"4"' in script, "VMTEST_CPUS should be 4")
    asserts.true(env, "VMTEST_NETWORK" in script, "script should set VMTEST_NETWORK")

    return analysistest.end(env)

basic_env_vars_test = analysistest.make(_basic_env_vars_test_impl)

# --- Test: vm_go_test with bridge networking sets bridge env vars ---

def _bridge_network_test_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    actions = analysistest.target_actions(env)
    write_actions = [a for a in actions if a.content != None]
    asserts.true(env, len(write_actions) > 0, "should have at least one write action")

    script = write_actions[0].content
    asserts.true(env, 'VMTEST_NETWORK="bridge"' in script, "should set VMTEST_NETWORK=bridge")
    asserts.true(env, 'VMTEST_BRIDGE="test-br0"' in script, "should set VMTEST_BRIDGE=test-br0")

    return analysistest.end(env)

bridge_network_test = analysistest.make(_bridge_network_test_impl)

# --- Test: vm_go_test with TPM sets TPM env vars ---

def _tpm_env_vars_test_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    actions = analysistest.target_actions(env)
    write_actions = [a for a in actions if a.content != None]
    asserts.true(env, len(write_actions) > 0, "should have at least one write action")

    script = write_actions[0].content
    asserts.true(env, 'VMTEST_TPM="true"' in script, "should set VMTEST_TPM=true")
    asserts.true(env, "VMTEST_SWTPM" in script, "should set VMTEST_SWTPM")
    asserts.true(env, "VMTEST_SWTPM_SETUP" in script, "should set VMTEST_SWTPM_SETUP")

    return analysistest.end(env)

tpm_env_vars_test = analysistest.make(_tpm_env_vars_test_impl)

# --- Test: vm_go_test includes test binary in runfiles ---

def _runfiles_test_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    runfiles = target[DefaultInfo].default_runfiles.files.to_list()
    basenames = [f.basename for f in runfiles]

    # The test binary should be in runfiles
    asserts.true(
        env,
        "mock_test" in basenames or "mock_test.sh" in basenames,
        "test binary should be in runfiles, got: " + str(basenames),
    )

    return analysistest.end(env)

runfiles_test = analysistest.make(_runfiles_test_impl)
