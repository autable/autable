# DingTalk approval comment

Posts a text comment onto an approval instance through
`POST /v1.0/workflow/processInstances/comments`. DingTalk has no notion of an
application commenting: every comment is posted as a user, so the node needs
the user id it should speak as. That user must be able to see the instance.

## Inputs

- `instance_id` — the approval instance.
- `text` — the comment body.
- `comment_user_id` — overrides the `comment_user_id` variable.

## Outputs

- `instance_id` and `comment_user_id` — echoed back once DingTalk has accepted
  the comment. A rejected comment is an error, not a false flag.

## Typical use

Write something back onto the approval once a workflow has acted on it — a
generated code, a reference number, a link — so the initiator and approvers
see it in the place they already look, without a separate notification.
Record on the row that the comment went out, so a rerun does not post it again.
