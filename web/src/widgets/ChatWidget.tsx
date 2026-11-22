import { ChatPanel } from '../components/ChatPanel'
import type { WidgetProps } from './registry'

export function ChatWidget(_: WidgetProps) {
  return <ChatPanel compact />
}
