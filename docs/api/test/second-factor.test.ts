// Contract tests for passkey, recovery, and TOTP second-factor routes:
// docs/design/second-factor-authentication.md,
// docs/design/passkey-second-factor-contract.md, and
// docs/design/totp-second-factor-contract.md. openapi.test.ts owns
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
  "/auth/second-factor/totp/verify",
  "/me/second-factor",
  "/me/second-factor/passkeys/options",
  "/me/second-factor/passkeys",
  "/me/second-factor/passkeys/{id}",
  "/me/second-factor/recovery-codes",
  "/me/second-factor/totp/enrollment",
  "/me/second-factor/totp",
] as const;

describe("second-factor contract (passkeys, recovery, TOTP)", () => {
  it("registers every passkey, recovery, and TOTP route", () => {
    for (const path of secondFactorPaths) {
      expect(path in doc.paths, path).toBe(true);
    }
    expect(doc.paths["/auth/second-factor/totp/verify"].post).toBeTruthy();
    expect(doc.paths["/me/second-factor/totp/enrollment"].post).toBeTruthy();
    expect(doc.paths["/me/second-factor/totp/enrollment"].put).toBeTruthy();
    expect(doc.paths["/me/second-factor/totp"].delete).toBeTruthy();
  });

  it("exposes passkeyEnrollment and totpEnrollment on capabilities, never account state", () => {
    const capabilities = doc.components.schemas.Capabilities;
    expect(capabilities.required).toContain("passkeyEnrollment");
    expect(capabilities.required).toContain("totpEnrollment");
    expect(capabilities.properties.passkeyEnrollment.type).toBe("boolean");
    expect(capabilities.properties.totpEnrollment.type).toBe("boolean");
    expect(capabilities.additionalProperties).toBe(false);
  });

  it("accepts exactly a six-digit code on the TOTP pending verify and enrollment completion routes", () => {
    const verifyCode =
      doc.components.schemas.TOTPVerifyRequest.properties.code;
    expect(verifyCode.pattern).toBe("^[0-9]{6}$");
    expect(doc.components.schemas.TOTPVerifyRequest.required).toEqual([
      "code",
    ]);
    expect(doc.components.schemas.TOTPVerifyRequest.additionalProperties).toBe(
      false,
    );

    const completeCode =
      doc.components.schemas.TOTPEnrollmentCompleteRequest.properties.code;
    expect(completeCode.pattern).toBe("^[0-9]{6}$");
    expect(
      doc.components.schemas.TOTPEnrollmentCompleteRequest.required,
    ).toEqual(["enrollmentId", "code"]);
  });

  it("gates TOTP enrollment start and completion behind TOTP_ENROLLMENT_ENABLED with the uniform 404", () => {
    for (const method of ["post", "put"] as const) {
      const op = doc.paths["/me/second-factor/totp/enrollment"][method];
      const description = JSON.stringify(op.responses["404"]);
      expect(description, method).toContain("TOTP_ENROLLMENT_ENABLED");
      expect(description, method).toContain("not_found");
    }
    // Verification, removal, and state ignore the flag: no 404 on those
    // routes documents it as a gate.
    expect(
      JSON.stringify(
        doc.paths["/auth/second-factor/totp/verify"].post.responses,
      ),
    ).not.toContain("TOTP_ENROLLMENT_ENABLED");
    expect(
      JSON.stringify(doc.paths["/me/second-factor/totp"].delete.responses),
    ).not.toContain("TOTP_ENROLLMENT_ENABLED");
  });

  it("shares the 4,096-byte password body cap on every TOTP route, not the WebAuthn cap", () => {
    const totpRoutes: Array<[string, "post" | "put"]> = [
      ["/auth/second-factor/totp/verify", "post"],
      ["/me/second-factor/totp/enrollment", "post"],
      ["/me/second-factor/totp/enrollment", "put"],
    ];
    for (const [path, method] of totpRoutes) {
      const ref = doc.paths[path][method].responses["413"].$ref;
      expect(ref, `${method.toUpperCase()} ${path}`).toBe(
        "#/components/responses/PasswordBodyTooLarge",
      );
    }
  });

  it("pins the per-account TOTP cool-down and shared attempt-budget rate shape on verify", () => {
    const ref =
      doc.paths["/auth/second-factor/totp/verify"].post.responses["429"].$ref;
    expect(ref).toBe(
      "#/components/responses/SecondFactorTOTPVerifyRateLimited",
    );
    const rateLimited = doc.components.responses.SecondFactorTOTPVerifyRateLimited;
    expect(rateLimited.headers["Retry-After"].schema.maximum).toBe(86400);
    expect(rateLimited.description).toContain("cool-down");
    expect(rateLimited.content["application/json"].example.error.code).toBe(
      "rate_limited",
    );
  });

  it("pins TOTP enrollment start and removal to the shared management limiter, and completion to both limiters", () => {
    expect(
      doc.paths["/me/second-factor/totp/enrollment"].post.responses["429"]
        .$ref,
    ).toBe("#/components/responses/SecondFactorManagementRateLimited");
    expect(doc.paths["/me/second-factor/totp"].delete.responses["429"].$ref).toBe(
      "#/components/responses/SecondFactorManagementRateLimited",
    );
    expect(
      doc.paths["/me/second-factor/totp/enrollment"].put.responses["429"]
        .$ref,
    ).toBe("#/components/responses/SecondFactorTOTPCompletionRateLimited");
  });

  it("reuses factor_not_found for TOTP pending verify and removal on an unenrolled account", () => {
    for (const [path, method] of [
      ["/auth/second-factor/totp/verify", "post"],
      ["/me/second-factor/totp", "delete"],
    ] as const) {
      expect(doc.paths[path][method].responses["404"].$ref, path).toBe(
        "#/components/responses/SecondFactorNotFound",
      );
    }
  });

  it("returns the TOTP secret and provisioning URI only once, never accepted back from the browser", () => {
    const start = doc.components.schemas.TOTPEnrollmentStartResponse;
    expect(start.properties.data.required).toEqual([
      "enrollmentId",
      "secret",
      "provisioningUri",
      "expiresAt",
    ]);
    expect(start.properties.data.properties.secret.pattern).toBe(
      "^[A-Z2-7]{4}( [A-Z2-7]{4}){7}$",
    );
    // No TOTP request body (verify or enrollment completion) accepts a
    // secret or provisioning URI back: only enrollmentId and code.
    expect(
      Object.keys(doc.components.schemas.TOTPVerifyRequest.properties),
    ).toEqual(["code"]);
    expect(
      Object.keys(
        doc.components.schemas.TOTPEnrollmentCompleteRequest.properties,
      ),
    ).toEqual(["enrollmentId", "code"]);
  });

  it("omits recoveryCodes on TOTP replacement, requiring only totpEnabled", () => {
    const complete = doc.components.schemas.TOTPEnrollmentCompleteResponse;
    expect(complete.properties.data.required).toEqual(["totpEnabled"]);
    expect(complete.properties.data.properties.totpEnabled.const).toBe(true);
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
      "totpEnabled",
      "recoveryCodesRemaining",
    ]);
    const methods = doc.components.schemas.SecondFactorPendingMethod;
    expect(methods.enum).toEqual(["passkey", "totp", "recovery"]);
    const pendingStatus = doc.components.schemas.SecondFactorPendingStatusResponse;
    expect(pendingStatus.properties.data.properties.methods.maxItems).toBe(3);
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
