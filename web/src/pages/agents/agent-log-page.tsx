import React from 'react';
import { useLocation, useParams } from 'react-router';
import { SessionFeedbackAuditView } from './session-feedback-audit-view';

const AgentLogPage: React.FC = () => {
  const { id: appId } = useParams();
  const location = useLocation();
  const isChatRoute = location.pathname.startsWith('/chat-log-page');

  return (
    <SessionFeedbackAuditView
      defaultSourceType={isChatRoute ? 'chat' : 'agent'}
      defaultAppId={appId || ''}
      defaultAppName=""
    />
  );
};

export default AgentLogPage;
