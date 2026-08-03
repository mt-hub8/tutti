import { ConfirmationDialog } from "@tutti-os/ui-system";
import { useTranslation } from "../../i18n/index";

/**
 * A transient confirmation for one capability invocation. It deliberately
 * holds no authorization state: durable consent is confirmed by the Host's
 * canonical session binding after the resulting submit succeeds.
 */
export function AgentCapabilityConsentDialog(props: {
  capabilityLabel: string;
  onConfirm: () => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
}): React.JSX.Element {
  const { t } = useTranslation();
  return (
    <ConfirmationDialog
      cancelLabel={t("agentHost.agentGui.capabilityConsent.cancel")}
      className="nodrag tsh-desktop-no-drag [-webkit-app-region:no-drag]"
      confirmLabel={t("agentHost.agentGui.capabilityConsent.confirm")}
      onConfirm={props.onConfirm}
      onOpenChange={props.onOpenChange}
      open={props.open}
      title={t("agentHost.agentGui.capabilityConsent.title", {
        capability: props.capabilityLabel
      })}
      description={t("agentHost.agentGui.capabilityConsent.description", {
        capability: props.capabilityLabel
      })}
    />
  );
}
