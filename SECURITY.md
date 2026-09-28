# Security policy

TrafficKit handles other people's secrets by design, so we take reports
seriously.

## Reporting

Please **don't** open a public issue for a vulnerability. Use GitHub's
private vulnerability reporting on this repository ("Security" tab →
"Report a vulnerability"). Include what you found, how to reproduce it, and
the version or commit.

You'll get an acknowledgement within a few days. We'll agree on a disclosure
date with you once a fix is ready, and credit you unless you'd rather not.

## Scope

Things we especially want to hear about:

- anything that lets another machine, a web page, or another local user
  reach the control API or read captured traffic
- captured content that can execute script in the UI
- captured data or secrets ending up in logs, files or the network
- crashes or unbounded memory use triggered by traffic passing through the
  proxy

The design is described in [docs/security/model.md](docs/security/model.md).

## Supported versions

Until 1.0, only the latest release gets fixes.
