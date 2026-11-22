import { ChatPanel } from '../components/ChatPanel'

export function AssistantPage() {
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow">
          <h1>AI assistant</h1>
          <p>Ask your own AI about your servers. It runs on your key, straight from this browser, and can read live dashboard data you choose to share. It cannot change anything.</p>
        </div>
      </div>
      <div className="card" style={{ height: 'min(640px, calc(100vh - 220px))', minHeight: 420 }}>
        <ChatPanel />
      </div>
    </div>
  )
}
