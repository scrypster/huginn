import { createRouter, createWebHashHistory } from 'vue-router'
import ChatView from '../views/ChatView.vue'
import AgentsView from '../views/AgentsView.vue'
import ModelsView from '../views/ModelsView.vue'
import ConnectionsView from '../views/ConnectionsView.vue'
import SettingsView from '../views/SettingsView.vue'
import LogsView from '../views/LogsView.vue'
import StatsView from '../views/StatsView.vue'
import InboxView from '../views/InboxView.vue'
import WorkflowsView from '../views/WorkflowsView.vue'
import CloudView from '../views/CloudView.vue'
import SkillsView from '../views/SkillsView.vue'

export default createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/chat' },
    { path: '/chat/:sessionId?', component: ChatView, props: true },
    { path: '/space/:spaceId', component: ChatView, props: true },
    { path: '/agents/:agentName?', component: AgentsView, props: true },
    { path: '/models/:provider?', component: ModelsView, props: true },
    { path: '/connections', component: ConnectionsView },
    { path: '/settings', component: SettingsView },
    { path: '/logs', component: LogsView },
    { path: '/stats', component: StatsView },
    { path: '/inbox', component: InboxView },
    { path: '/routines', redirect: '/workflows' },
    { path: '/workflows/:id?', component: WorkflowsView, props: true },
    { path: '/cloud', component: CloudView },
    { path: '/skills/:tab?', component: SkillsView, props: true },
  ],
})
