import type { ComponentType } from 'react'

export interface TabItem {
  key: string
  label: string
}

interface TabViewProps {
  tabs: TabItem[]
  views: Record<string, ComponentType>
  activeTab: string
  baseHref: string
  defaultTab?: string
}

export function TabView({ tabs, views, activeTab, baseHref, defaultTab }: TabViewProps) {
  const currentTab = activeTab || defaultTab || tabs[0]?.key
  const CurrentView = views[currentTab]

  return (
    <div>
      <div role="tablist" className="flex gap-1 border-b border-surface-200 mb-6 flex-wrap">
        {tabs.map((tab) => (
          <a
            key={tab.key}
            role="tab"
            aria-selected={currentTab === tab.key}
            href={`${baseHref}/${tab.key}`}
            className={`px-4 py-2.5 text-sm font-medium border-b-2 transition-colors ${
              currentTab === tab.key
                ? 'border-brand-600 text-brand-700'
                : 'border-transparent text-surface-500 hover:text-surface-700 hover:border-surface-300'
            }`}
          >
            {tab.label}
          </a>
        ))}
      </div>
      {CurrentView && <CurrentView />}
    </div>
  )
}
