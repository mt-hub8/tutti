import {
  getTuttidErrorI18nCandidates,
  normalizeTuttidError
} from "@tutti-os/client-tuttid-ts";
import { classifyDesktopErrorCode } from "../../../shared/errors/desktopErrors.ts";
import {
  createDesktopErrorI18nRuntime,
  defaultDesktopLocale,
  type DesktopLocale
} from "../../../shared/i18n/index.ts";

export type DesktopErrorMessageOverrides = Partial<Record<string, string>>;

export function resolveDesktopErrorMessage(
  error: unknown,
  locale: DesktopLocale = defaultDesktopLocale,
  overrides: DesktopErrorMessageOverrides = {}
): string {
  const copy = createDesktopErrorI18nRuntime(locale);
  const unexpectedServiceError = () =>
    copy.t("errors.transport_request_failed");
  const protocolError = normalizeTuttidError(error);
  if (protocolError) {
    const params = protocolError.params as Record<
      string,
      string | number | boolean | null | undefined
    >;
    const overrideMessage = overrides[protocolError.code];
    if (overrideMessage) {
      return overrideMessage;
    }

    for (const candidate of getTuttidErrorI18nCandidates(protocolError)) {
      const candidateOverride = overrides[candidate];
      if (candidateOverride) {
        return candidateOverride;
      }

      if (copy.has(candidate)) {
        return copy.t(candidate, params);
      }
    }

    return unexpectedServiceError();
  }

  const explicitDesktopErrorCode = getDesktopErrorCode(error);
  const desktopErrorCode =
    explicitDesktopErrorCode ??
    (error instanceof Error ? classifyDesktopErrorCode(error) : null);
  if (!desktopErrorCode) {
    return error instanceof Error
      ? unexpectedServiceError()
      : copy.t("common.unknownError");
  }

  const desktopOverride = overrides[desktopErrorCode];
  if (desktopOverride) {
    return desktopOverride;
  }

  const desktopKey = `errors.${desktopErrorCode}`;
  if (copy.has(desktopKey)) {
    return copy.t(desktopKey);
  }

  return unexpectedServiceError();
}

export function wrapLocalizedTuttidErrorIfSpecific(
  error: unknown,
  locale: DesktopLocale = defaultDesktopLocale
): unknown {
  const protocolError = normalizeTuttidError(error);
  if (!protocolError?.reason) {
    return error;
  }

  const copy = createDesktopErrorI18nRuntime(locale);
  const reasonKey = `errors.${protocolError.code}.${protocolError.reason}`;
  if (!copy.has(reasonKey)) {
    return error;
  }

  const message = resolveDesktopErrorMessage(error, locale);
  const wrapped = new Error(message);
  wrapped.name = error instanceof Error ? error.name : "TuttidProtocolError";
  Object.assign(wrapped, {
    code: protocolError.code,
    correlationId: protocolError.correlationId,
    developerMessage: protocolError.developerMessage,
    params: protocolError.params,
    reason: protocolError.reason,
    retryable: protocolError.retryable,
    statusCode: protocolError.statusCode,
    turnCapabilityOutcome: protocolError.turnCapabilityOutcome
  });
  return wrapped;
}

export function getDesktopErrorCode(error: unknown): string | null {
  const protocolError = normalizeTuttidError(error);
  if (protocolError) {
    return protocolError.code;
  }

  if (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    typeof (error as { code?: unknown }).code === "string"
  ) {
    return (error as { code: string }).code;
  }

  return null;
}
