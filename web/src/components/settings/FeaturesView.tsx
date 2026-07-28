import { BudgetManagementView } from '../budget/BudgetManagementView'
import { CircuitBreakerView } from '../circuit-breaker/CircuitBreakerView'
import { FallbackView } from '../fallback/FallbackView'
import { FeatureFlagsView } from '../features/FeatureFlagsView'
import { GuardrailsView } from '../guardrails/GuardrailsView'
import { LoopDetectionView } from '../loop-detection/LoopDetectionView'
import { PiiProtectionView } from '../pii-protection/PiiProtectionView'
import { RateLimitingView } from '../rate-limiting/RateLimitingView'
import { SemanticCacheView } from '../semantic-cache/SemanticCacheView'
import { StrategiesView } from '../strategy/StrategiesView'
import { ErrorBoundary } from '../ui/ErrorBoundary'
import type { TabItem } from '../ui/TabView'
import { TabView } from '../ui/TabView'

export const FEATURE_TABS: TabItem[] = [
  { key: 'budget', label: 'Budget' },
  { key: 'pii-protection', label: 'PII Protection' },
  { key: 'smart-router', label: 'Smart Router' },
  { key: 'rate-limit', label: 'Rate Limiting' },
  { key: 'guardrails', label: 'Guardrails' },
  { key: 'loop-detection', label: 'Loop Detection' },
  { key: 'semantic-cache', label: 'Semantic Cache' },
  { key: 'circuit-breaker', label: 'Circuit Breaker' },
  { key: 'fallback', label: 'Fallback' },
  { key: 'features', label: 'Feature Flags' },
]

export type FeatureTab = (typeof FEATURE_TABS)[number]['key']
export const DEFAULT_FEATURE_TAB: FeatureTab = 'budget'

const VIEWS = {
  budget: BudgetManagementView,
  'pii-protection': PiiProtectionView,
  'smart-router': StrategiesView,
  'rate-limit': RateLimitingView,
  guardrails: GuardrailsView,
  'loop-detection': LoopDetectionView,
  'semantic-cache': SemanticCacheView,
  'circuit-breaker': CircuitBreakerView,
  fallback: FallbackView,
  features: FeatureFlagsView,
} as const

export function FeaturesView({ initialTab }: { initialTab?: FeatureTab }) {
  return (
    <ErrorBoundary>
      <TabView tabs={FEATURE_TABS} views={VIEWS} activeTab={initialTab || DEFAULT_FEATURE_TAB} baseHref="/features" />
    </ErrorBoundary>
  )
}
