import { ModelsView } from '../models/ModelsView'
import { PromptsView } from '../prompts/PromptsView'
import { ProvidersView } from '../providers/ProvidersView'
import type { TabItem } from '../ui/TabView'
import { TabView } from '../ui/TabView'

export type Tab = 'providers' | 'models' | 'prompts'

export const TABS: TabItem[] = [
  { key: 'providers', label: 'Providers' },
  { key: 'models', label: 'Models' },
  { key: 'prompts', label: 'Prompts' },
]

const VIEWS = {
  providers: ProvidersView,
  models: ModelsView,
  prompts: PromptsView,
} as const

export function LlmView({ initialTab }: { initialTab?: Tab }) {
  return <TabView tabs={TABS} views={VIEWS} activeTab={initialTab || 'providers'} baseHref="/llm" />
}
