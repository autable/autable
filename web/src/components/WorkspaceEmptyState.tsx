import { Text } from "@fluentui/react-components";
import type { ReactNode } from "react";

export function WorkspaceEmptyState({
  icon,
  title,
  description
}: {
  icon: ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div className="workspace-empty">
      <div className="workspace-empty-card">
        <div className="workspace-empty-icon" aria-hidden="true">
          {icon}
        </div>
        <Text weight="semibold" size={400}>
          {title}
        </Text>
        <Text size={200} align="center">
          {description}
        </Text>
      </div>
    </div>
  );
}
