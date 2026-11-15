# Production release fence

The release fence stops the supported production path from starting an image
below the minimum release once second-factor enrollment can exist
([ADR 0017](../adr/0017-second-factor-authentication.md)). These feature floors
apply:

| Enrollment flag              | Floor   | Numeric release |
| ---------------------------- | ------- | --------------- |
| `PASSKEY_ENROLLMENT_ENABLED` | v0.4.2  | 4002            |
| `TOTP_ENROLLMENT_ENABLED`    | v0.4.7  | 4007            |
| `SIGN_IN_TO_VIEW_ENABLED`    | v0.6.26 | 6026            |

Production uses the [Vietnam host fence](vietnam-production.md#release-fence)
and the [host commands](../runbooks/production.md#passkey-release-fence). Host
TOTP re-encryption remains an
[open host procedure](../runbooks/totp-keys.md#key-rotation).

The fence does not constrain privileged host administration.
