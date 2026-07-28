export type ScopeLevel = 'key' | 'user' | 'group'

interface LevelScopeSelectorProps {
  value: ScopeLevel
  onChange: (next: ScopeLevel) => void
}

const labels: Record<ScopeLevel, string> = {
  key: 'API Key Level',
  user: 'User Level',
  group: 'Group Level',
}

export function LevelScopeSelector({ value, onChange }: LevelScopeSelectorProps) {
  return (
    <div className="flex gap-1 rounded-lg bg-surface-100 p-1 self-start">
      {(['key', 'user', 'group'] as const).map((tab) => (
        <button
          key={tab}
          type="button"
          onClick={() => onChange(tab)}
          className={`px-4 py-2 text-sm font-medium rounded-md transition-colors ${
            value === tab ? 'bg-white text-surface-900 shadow-sm' : 'text-surface-500 hover:text-surface-700'
          }`}
        >
          {labels[tab]}
        </button>
      ))}
    </div>
  )
}
