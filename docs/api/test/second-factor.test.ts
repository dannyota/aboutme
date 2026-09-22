// Contract tests for the v0.4.2 passkey second factor:
// docs/design/second-factor-authentication.md and
// docs/design/passkey-second-factor-contract.md. openapi.test.ts owns
// cross-cutting, whole-document invariants; this file owns the second-factor
// surface specifically.
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));

const secondFactorPaths = [
  "/auth/second-factor",
  "/auth/second-factor/passkey/options",
  "/auth/second-factor/passkey/verify",
  "/auth/second-factor/recovery/verify",
  "/me/second-factor",
  "/me/second-factor/passkeys/options",
  "/me/second-factor/passkeys",
  "/me/second-factor/passkeys/{id}",
  "/me/second-factor/recovery-codes",
] as const;

describe("second-factor contract (v0.4.2 passkeys)", () => {
  it("registers every v0.4.2 route and no TOTP route or field", () => {
    for (const path of secondFactorPaths) {
      expect(path in doc.paths, path).toBe(true);
    }
    const totpPaths = Object.keys(doc.paths).filter((p) =>
      p.toLowerCase().includes("totp"),
    );
    expect(totpPaths).toEqual([]);
    expect(JSON.stringify(doc)).not.toMatch(/totpEnabled|totpEnrollment/);
  });

  it("exposes passkeyEnrollment on capabilities, never account state", () => {
    const capabilities = doc.components.schemas.Capabilities;
    expect(capabilities.required).toContain("passkeyEnrollment");
    expect(capabilities.properties.passkeyEnrollment.type).toBe("boolean");
    expect(capabilities.additionalProperties).toBe(false);
  });

  it("accepts an optional same-origin next field on password login", () => {
    const next = doc.components.schemas.PasswordLoginRequest.properties.next;
    expect(next.maxLength).toBe(2048);
    expect(next.pattern).toBe("^/(?!/)[^\\\\]*$");
    expect(doc.components.schemas.PasswordLoginRequest.required).toEqual([
      "email",
      "password",
    ]);
  });

  it("pins the 202 secondFactorRequired pending response on login and reauth", () => {
    const login = doc.paths["/auth/password/login"].post;
    const reauth = doc.paths["/auth/password/reauth"].post;
    expect(login.responses["202"].$ref).toBe(
      "#/components/responses/PasswordSecondFactorRequired",
    );
    expect(reauth.responses["202"].$ref).toBe(
      "#/components/responses/PasswordSecondFactorRequired",
    );
    const pending = doc.components.responses.PasswordSecondFactorRequired;
    const example = pending.content["application/json"].example;
    expect(example.data.secondFactorRequired).toBe(true);
    const schema = doc.components.schemas.PasswordSecondFactorRequiredResponse;
    expect(schema.properties.data.properties.secondFactorRequired.const).toBe(
      true,
    );
  });

  it("documents the provider callback's redirect to /login/second-factor for an enrolled account", () => {
    const description = doc.paths["/auth/google/callback"].get.description;
    expect(description).toContain("/login/second-factor");
    expect(description).toContain("__Host-auth-pending");
    expect(description).toMatch(/purpose=login.*purpose=reauth/s);
  });

  it("requires reauth_required on POST /oauth/consent for an enrolled approval, not denial", () => {
    const forbidden = doc.paths["/oauth/consent"].post.responses["403"];
    const examples = forbidden.content["application/json"].examples;
    expect(examples.reauth_required.value.error.code).toBe("reauth_required");
    expect(examples.csrf_rejected.value.error.code).toBe("csrf_rejected");
    expect(forbidden.description).toMatch(/approve/);
    expect(forbidden.description).toMatch(/deny.*never/is);
  });

  it("pins passkey registration options' and completion's 409 passkey_limit_reached", () => {
    for (const path of [
      "/me/second-factor/passkeys/options",
      "/me/second-factor/passkeys",
    ]) {
      const op = doc.paths[path].post;
      expect(op.responses["409"].$ref, path).toBe(
        "#/components/responses/SecondFactorLimitReached",
      );
    }
    const limit = doc.components.responses.SecondFactorLimitReached;
    expect(limit.content["application/json"].example.error.code).toBe(
      "passkey_limit_reached",
    );
  });

  it("pins registration completion's 400 verification_failed for a failed WebAuthn proof or duplicate credential", () => {
    const body = JSON.stringify(
      doc.paths["/me/second-factor/passkeys"].post.responses["400"],
    );
    expect(body).toContain("verification_failed");
    expect(body).toContain("challenge_invalid");
    expect(body).toContain("duplicate credential");
  });

  it("pins rate_limited with Retry-After on every /me/second-factor mutation route", () => {
    const mutations: Array<[string, "post" | "delete"]> = [
      ["/me/second-factor/passkeys/options", "post"],
      ["/me/second-factor/passkeys", "post"],
      ["/me/second-factor/passkeys/{id}", "delete"],
      ["/me/second-factor/recovery-codes", "post"],
    ];
    for (const [path, method] of mutations) {
      const ref = doc.paths[path][method].responses["429"].$ref;
      expect(ref, `${method.toUpperCase()} ${path}`).toBe(
        "#/components/responses/SecondFactorManagementRateLimited",
      );
    }
    const limited = doc.components.responses.SecondFactorManagementRateLimited;
    expect(limited.headers["Retry-After"].schema.type).toBe("integer");
    expect(limited.content["application/json"].example.error.code).toBe(
      "rate_limited",
    );
    expect(limited.description).toContain("10-per-hour");
  });

  it("pins the pending cookie and its distinct CSRF token on every pending route", () => {
    const schemes = doc.components.securitySchemes;
    expect(schemes.pendingCookie.name).toBe("__Host-auth-pending");
    expect(schemes.pendingCsrfToken.name).toBe("X-CSRF-Token");
    expect(schemes.pendingCsrfToken.name).toBe(schemes.csrfToken.name);

    expect(doc.paths["/auth/second-factor"].get.security).toEqual([
      { pendingCookie: [] },
    ]);
    for (const path of [
      "/auth/second-factor/passkey/options",
      "/auth/second-factor/passkey/verify",
      "/auth/second-factor/recovery/verify",
    ]) {
      expect(doc.paths[path].post.security).toEqual([
        { pendingCookie: [], pendingCsrfToken: [] },
      ]);
    }
  });

  it("bounds WebAuthn wire fields to the budgeted decoded-byte ceilings", () => {
    const registration =
      doc.components.schemas.PasskeyRegistrationCompletionRequest.properties
        .credential.properties.response.properties;
    expect(registration.clientDataJSON.description).toContain("4,096");
    expect(registration.attestationObject.description).toContain("16,384");
    expect(registration.transports.maxItems).toBe(8);
    expect(registration.transports.items.maxLength).toBe(32);

    const assertion =
      doc.components.schemas.PasskeyAssertionCompletionRequest.properties
        .credential.properties.response.properties;
    expect(assertion.clientDataJSON.description).toContain("4,096");
    expect(assertion.authenticatorData.description).toContain("4,096");
    expect(assertion.signature.description).toContain("1,024");
    expect(assertion.userHandle.type).toEqual(["string", "null"]);

    const rawId =
      doc.components.schemas.PasskeyRegistrationCompletionRequest.properties
        .credential.properties.rawId;
    expect(rawId.description).toContain("16 to 1,023");
  });

  it("uses the WebAuthn-specific 32,768-byte body cap, not the 4,096-byte password cap", () => {
    const webAuthnRoutes: Array<[string, "post"]> = [
      ["/auth/second-factor/passkey/options", "post"],
      ["/auth/second-factor/passkey/verify", "post"],
      ["/me/second-factor/passkeys/options", "post"],
      ["/me/second-factor/passkeys", "post"],
      ["/me/second-factor/recovery-codes", "post"],
    ];
    for (const [path, method] of webAuthnRoutes) {
      const ref = doc.paths[path][method].responses["413"].$ref;
      expect(ref, `${method.toUpperCase()} ${path}`).toBe(
        "#/components/responses/SecondFactorWebAuthnBodyTooLarge",
      );
    }
    // The recovery pending-verify route genuinely shares the password
    // routes' 4,096-byte cap (docs/design/budgets.md, "Recovery
    // verification request body"), so it keeps the shared response.
    expect(
      doc.paths["/auth/second-factor/recovery/verify"].post.responses["413"]
        .$ref,
    ).toBe("#/components/responses/PasswordBodyTooLarge");
    expect(
      doc.components.responses.SecondFactorWebAuthnBodyTooLarge.description,
    ).toContain("32,768");
  });

  it("closes the recovery-code display format to Crockford Base32 in five-character groups", () => {
    const recoveryCode = doc.components.schemas.SecondFactorRecoveryCode;
    expect(recoveryCode.pattern).toBe(
      "^amr_[0-9A-HJKMNP-TV-Z]{5}(-[0-9A-HJKMNP-TV-Z]{5}){4}-[0-9A-HJKMNP-TV-Z]$",
    );
    const registrationCodes =
      doc.components.schemas.SecondFactorRegistrationResponse.properties.data
        .properties.recoveryCodes;
    expect(registrationCodes.minItems).toBe(10);
    expect(registrationCodes.maxItems).toBe(10);
    const regeneratedCodes =
      doc.components.schemas.SecondFactorRecoveryCodesResponse.properties.data
        .properties.recoveryCodes;
    expect(regeneratedCodes.minItems).toBe(10);
    expect(regeneratedCodes.maxItems).toBe(10);
  });

  it("marks nullable fields [type, null] rather than an absent format", () => {
    const passkey = doc.components.schemas.SecondFactorPasskey;
    expect(passkey.required).toEqual(["id", "createdAt", "lastUsedAt"]);
    expect(passkey.properties.lastUsedAt.type).toEqual(["string", "null"]);
  });

  it("derives enabled from the same read-only snapshot, with a closed method order", () => {
    const state = doc.components.schemas.SecondFactorStateResponse;
    expect(state.properties.data.required).toEqual([
      "enabled",
      "passkeys",
      "recoveryCodesRemaining",
    ]);
    const methods = doc.components.schemas.SecondFactorPendingMethod;
    expect(methods.enum).toEqual(["passkey", "recovery"]);
  });

  it("pins the fixed WebAuthn registration and assertion option shapes", () => {
    const registration = doc.components.schemas.WebAuthnRegistrationPublicKey;
    expect(registration.properties.timeout.const).toBe(300000);
    expect(registration.properties.attestation.const).toBe("none");
    expect(
      registration.properties.authenticatorSelection.properties.residentKey
        .const,
    ).toBe("required");
    expect(
      registration.properties.pubKeyCredParams.items.properties.alg.enum,
    ).toEqual([-7, -257]);

    const assertion = doc.components.schemas.WebAuthnAssertionPublicKey;
    expect(assertion.properties.timeout.const).toBe(300000);
    expect(assertion.properties.userVerification.const).toBe("required");
    expect(assertion.properties.allowCredentials.minItems).toBe(1);

    const transport = doc.components.schemas.WebAuthnTransport;
    expect(transport.enum).toEqual([
      "usb",
      "nfc",
      "ble",
      "smart-card",
      "hybrid",
      "internal",
    ]);
  });
});
