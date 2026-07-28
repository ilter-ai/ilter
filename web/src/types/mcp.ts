export interface ServerVariable {
  key: string
  label: string
  description?: string
  default?: string
  secret?: boolean
  required?: boolean
}

export interface MCPServerEntry {
  name: string
  package: string
  description: string
  category: string
  command: string
  args: string[]
  tools: string[]
  variables: ServerVariable[]
  stars: number
  url: string
}
