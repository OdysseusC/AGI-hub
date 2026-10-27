# Grok lab offline recovery

[简体中文](RECOVERY.md) | [English](RECOVERY.en.md)

`recovery.py` is a one-shot maintenance entry. It neither claims Hub mail nor invokes the regular mailbox adapter. It applies only to the same already authorized Routine and the existing lab on the selected computer. Normal collaboration continues through the [Hub mailbox](README.en.md).

**Rejected by default; native maintenance acceptance remains pending.** A Routine supporting only business mailboxes cannot use this entry. Some official UI versions make “Edit” generate a chat draft instead of providing a separate save form. Opening the editor, copying a webhook or saving local configuration does not prove the branch is loaded. Check the actual official configuration mechanism and the recovery path permitted by the user first.

## Separate configuration and plan

All values below are examples. Keep files in a private directory outside the repository and check the actual target, existing RootEntry, and current human authorization. Never commit URLs, keys, or private paths. Record `maintenance_contract` only after the local administrator has added and verified the independent branch through an actual official configuration mechanism. A fabricated `evidence_ref` must not unlock it.

`recovery-config.json`:

```json
{
  "maintenance_contract": {
    "version": 1,
    "routine_id": "existing-authorized-routine",
    "evidence_ref": "<actual-operator-verification-record>"
  },
  "native": {
    "routine_id": "existing-authorized-routine",
    "url": "https://existing-routine.example.invalid/webhook",
    "headers": {"Authorization": "Bearer <existing-webhook-key>"},
    "timeout_seconds": 30
  }
}
```

The contract has exactly three fields: integer `version:1`, a `routine_id` matching `native.routine_id` exactly, and a nonempty `evidence_ref` without control characters, up to 256 characters. The reference is an operator record, never a path the program reads. Use the existing Routine's HTTPS URL; do not create credentials or permissions. Native fields may instead appear at the top level alongside the contract. A full Client configuration may be read as a container, but `endpoint_token` is neither required nor used. This separate recovery configuration must not replace a regular consumer configuration. Existing `native.body` is ignored: the program generates only the `agi_hub_lab_recovery` field, with no free-form prompt or body-template interface.

`plan.json`:

```json
{
  "type": "agi_hub_lab_recovery",
  "source": "out_of_band_maintenance",
  "hub_claimed": false,
  "attempt_id": "lab-recovery-20261027-01",
  "expires_at": "2026-10-27T12:15:00Z",
  "target": {
    "hub_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "device_id": "windows-lab",
    "data_dir": "C:\\Users\\owner\\existing-lab-hub"
  },
  "root_entry": {
    "hub_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "endpoint": "codex"
  },
  "original_work_ref": "human-recovery-authorization-20261027"
}
```

Every key is required and extra keys are rejected. The first three values are fixed. Hub IDs are 64 lowercase hexadecimal characters. `data_dir` must be the known lab's absolute, non-root directory without `..`. Use identifiers accepted by the Hub for the device, endpoint, and attempt. `original_work_ref` is a short reference to the current human authorization, not instructions or credentials. **Replace the example deadline:** it must be future UTC, no more than 24 hours away. Expired plans are rejected; maintenance cannot execute an earlier reboot request.

## One call and its result

Only after the actual verification above, run once from the repository root. All three file arguments must be absolute paths:

```sh
python3 clients/grok-routine/recovery.py --config /absolute/private/recovery-config.json --plan /absolute/private/plan.json --attempt-record /absolute/private/lab-recovery-20261027-01.json
```

On Windows, use `py -3` and quoted absolute paths on that computer. Before its single `post_json_once` call, the program creates a private attempt file with `O_EXCL`, writes `sending` and the actual payload's SHA-256, and calls `fsync`. The record and terminal output contain no URL, key, or request body.

Duplicate prevention applies **only to the specified attempt file**, not to official server-side idempotency. Renaming the file, deleting it, or recreating the same ID breaks that local barrier. Every existing record is rejected. Timeouts, non-2xx responses, and uncertain result persistence never trigger automatic resends. Preserve the original record and reconcile native evidence first. `transport_accepted`, including HTTP 202, means only HTTP transport acceptance; execution remains unverified. Neither `uncertain`, `not_sent`, nor `rejected` means recovery succeeded. The program never fabricates a Hub receipt, business reply, or Report.

## Independent Routine branch for administrator review

This instruction template must be installed and verified in the target environment. The administrator must check existing skills, the fixed helper, tool permissions, and how the actual instruction is loaded. If no official configuration mechanism is available, stop; sending a chat draft does not configure a Routine. Preserve existing business branches and handle maintenance independently:

```text
Enter this maintenance branch only when the webhook JSON contains exactly the
agi_hub_lab_recovery field. Never route it through grok_pr9_mailbox or treat it
as a claimed Hub request. Check the enclosed plan's type=agi_hub_lab_recovery,
source=out_of_band_maintenance, hub_claimed=false, unexpired expires_at, and
the existing local lab's hub_id/device_id/data_dir. Stop on any mismatch.
Persist attempt_id before side effects; repeated IDs do no further work.

Use only this lab's existing skills, fixed tools, and registered commands to
inspect and restore the original lab Hub, its enabled registered Clients,
and diagnostics. Attach to healthy processes and preserve production.
Do not reboot/shut down, claim/replay/execute old business mail, change ACLs,
credentials, verification, host permissions or login items, copy IPC, start
Claude, or create other assistant sessions. Missing existing tools, real host
context, or required proxy verification means needs_host; stop that recovery.

After Hub recovery, use only this endpoint's existing fixed helper to start
a new Hub conversation to root_entry reporting attempt_id, original_work_ref,
actual boot time, Hub/Client observations, and remaining blockers. Include no
credentials or business content. Never fabricate receipts/Reports or replace
the report with window chat or HTTP success. While the Hub is unavailable,
retain local diagnostics without expanding permissions or adding a channel.
```

Offline check: `python3 clients/grok-routine/test_recovery.py -v`. Passing tests do not prove that the selected Routine can access the target computer or complete native recovery.
