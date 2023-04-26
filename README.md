# GitHub Milestone Manager (GHMM)

GHMM lets you easily manage milestones, including bulk-management across an entire GitHub org.

Examples:

```bash
# List all milestines in the ACMECorp organization:
$ ghmm -t <TOKEN> list acmecorp

# Create a new milestone, M42, across all repos in the ACMECorp organization:
$ ghmm -t <TOKEN> open acmecorp M42 '7/1/2019'

# Change milestone M42's end date to 8/1/2019 across all repos in the ACMECorp organization:
$ ghmm -t <TOKEN> set acmecorp M42 '8/1/2019'

# Close out the M42 milestone across all repos in the ACMECorp organization:
$ ghmm -t <TOKEN> close acmecorp M42
```

Although these examples show bulk-editing across an organization, a single repo may be passed instead.

`<TOKEN>` must be a GitHub access token with sufficient rights to perform the operation.

In all examples, the command defaults to a dry-run; to actually commit the changes, pass `--yes` (`-y` for short).
