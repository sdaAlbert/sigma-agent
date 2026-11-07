# SWE-bench Verified Mini configs

- [`full-sigma-5.json`](full-sigma-5.json) is the study's shared full-Sigma
  runtime config.
- [`pilot.json`](pilot.json) is the earlier pilot config with Skills and memory
  disabled.
- [`task-overrides/`](task-overrides/) contains 14 Django task-specific
  configurations. These customize command images and shared-profile paths;
  they do not cover every one of the 50 task IDs.

The preparation scripts use `full-sigma-5.json` by default. When a task needs a
task-specific image or external profile, pass the matching task override and
record the config SHA-256, Sigma commit, model, and all run limits. Relative
paths in task overrides resolve from each config's directory.

These are the only frozen run configs in the primary Mini study folder. Do not
infer a single score across batches that used different models, prompt/runtime
versions, profiles, or budgets.
