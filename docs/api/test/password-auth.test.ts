import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));

const PASSWORD_OPS = {
  "/auth/password/register": { method: "post", op: "postAuthPasswordRegister" },
  "/auth/password/verify": { method: "post", op: "postAuthPasswordVerify" },
  "/auth/password/login": { method: "post", op: "postAuthPasswordLogin" },
  "/auth/password/forgot": { method: "post", op: "postAuthPasswordForgot" },
  "/auth/password/reset": { method: "post", op: "postAuthPasswordReset" },
  "/auth/password/reauth": { method: "post", op: "postAuthPasswordReauth" },
  "/me/password": { method: "put", op: "putMePassword" },
} as const;

const statusMatrix = {
  "/auth/password/register": ["202", "400", "403", "413", "415", "422", "429", "503"],
  "/auth/password/verify": ["204", "400", "403", "413", "415", "429", "503"],
  "/auth/password/login": ["204", "400", "401", "403", "413", "415", "429", "503"],
  "/auth/password/forgot": ["202", "400", "403", "413", "415", "429", "503"],
  "/auth/password/reset": ["204", "400", "403", "413", "415", "422", "429", "503"],
  "/auth/password/reauth": ["204", "400", "401", "403", "413", "415", "429", "503"],
  "/me/password": ["204", "400", "401", "403", "413", "415", "422", "429", "503"],
} as const;

describe("password auth contract", () => {
  it("defines all seven operations with the exact operationIds", () => {
    for (const [path, { method, op }] of Object.entries(PASSWORD_OPS)) {
      const operation = doc.paths[path]?.[method];
      expect(operation, `${method.toUpperCase()} ${path}`).toBeTruthy();
      expect(operation.operationId, `${method.toUpperCase()} ${path}`).toBe(op);
    }
  });

  it("documents the exact closed status set for every operation", () => {
    for (const [path, statuses] of Object.entries(statusMatrix)) {
      const method = PASSWORD_OPS[path as keyof typeof PASSWORD_OPS].method;
      const responses = doc.paths[path][method].responses;
      expect(Object.keys(responses).sort(), path).toEqual([...statuses].sort());
    }
  });

  it("marks only reauth and set/change as authenticated", () => {
    for (const [path, { method }] of Object.entries(PASSWORD_OPS)) {
      const security = doc.paths[path][method].security;
      const isAuthed =
        path === "/auth/password/reauth" || path === "/me/password";
      expect(security, path).toEqual(
        isAuthed ? [{ sessionCookie: [], csrfToken: [] }] : [],
      );
    }
  });

  it("requires hasPassword on the /me user", () => {
    const user = doc.components.schemas.User;
    expect(user.required).toContain("hasPassword");
    expect(user.properties.hasPassword.type).toBe("boolean");
  });

  it("keeps every request schema closed and non-null", () => {
    const required: Record<string, string[]> = {
      PasswordRegisterRequest: ["name", "email", "password"],
      PasswordVerifyRequest: ["token"],
      PasswordLoginRequest: ["email", "password"],
      PasswordForgotRequest: ["email"],
      PasswordResetRequest: ["token", "password"],
      PasswordReauthRequest: ["password"],
      PasswordSetRequest: ["password"],
    };
    for (const [name, fields] of Object.entries(required)) {
      const schema = doc.components.schemas[name];
      expect(schema.additionalProperties, name).toBe(false);
      expect(schema.required, name).toEqual(fields);
    }
  });

  it("uses one fixed 202 body for register and forgot", () => {
    const accepted = doc.components.schemas.PasswordAccepted;
    expect(accepted.required).toEqual(["data"]);
    expect(accepted.properties.data.required).toEqual(["accepted"]);
    expect(accepted.properties.data.properties.accepted.type).toBe("boolean");
    expect(doc.paths["/auth/password/register"].post.responses["202"].$ref).toBe(
      "#/components/responses/PasswordAccepted",
    );
    expect(doc.paths["/auth/password/forgot"].post.responses["202"].$ref).toBe(
      "#/components/responses/PasswordAccepted",
    );
  });

  it("closes the password policy issue enum", () => {
    expect(doc.components.schemas.PasswordPolicyIssue.enum).toEqual([
      "length",
      "common",
      "breached",
    ]);
  });

  it("defines the token as exactly 43 base64url characters", () => {
    const token = doc.components.schemas.PasswordToken;
    expect(token.minLength).toBe(43);
    expect(token.maxLength).toBe(43);
  });

  it("forbids sensitive property names in the password contract", () => {
    const names = [
      "PasswordRegisterRequest",
      "PasswordVerifyRequest",
      "PasswordLoginRequest",
      "PasswordForgotRequest",
      "PasswordResetRequest",
      "PasswordReauthRequest",
      "PasswordSetRequest",
      "PasswordAccepted",
      "PasswordEmail",
      "PasswordValue",
      "PasswordToken",
    ];
    const forbidden =
      /confirmation|providerEmail|provider_email|rawToken|raw_token|hash|mailJob|keyId|tokenState/i;
    for (const name of names) {
      const props = Object.keys(doc.components.schemas[name].properties ?? {});
      for (const key of props) {
        expect(forbidden.test(key), `${name}.${key}`).toBe(false);
      }
    }
  });
});
