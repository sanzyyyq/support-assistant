import { ChatWindow } from '../components/ChatWindow';

/** Полноэкранный чат (мобильные, встраивание через iframe) */
export function ChatPage() {
  return <ChatWindow variant="page" />;
}
