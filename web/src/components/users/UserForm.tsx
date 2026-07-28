import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { api } from '../../lib/api'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { QueryProvider } from '../ui/query-provider'

interface UserFormProps {
  userId?: string
  onSaved?: () => void
  onCancel?: () => void
}

function UserFormContent({ userId, onSaved, onCancel }: UserFormProps) {
  const isEdit = !!userId

  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [status, setStatus] = useState('active')
  const [saving, setSaving] = useState(false)
  const [nameError, setNameError] = useState('')
  const [emailError, setEmailError] = useState('')
  const [password, setPassword] = useState('')
  const [passwordError, setPasswordError] = useState('')

  const { isLoading, error: fetchError } = useQuery({
    queryKey: qk.userDetail(Number(userId)),
    queryFn: async () => {
      const user = await api.users.getUser(Number(userId))
      setName(user.name)
      setEmail(user.email)
      setStatus(user.status || 'active')
      return user
    },
    enabled: isEdit,
  })

  const validate = (): boolean => {
    let valid = true

    if (!name.trim()) {
      setNameError('Name is required')
      valid = false
    } else {
      setNameError('')
    }

    if (!email.trim()) {
      setEmailError('Email is required')
      valid = false
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      setEmailError('Invalid email format')
      valid = false
    } else {
      setEmailError('')
    }

    if (!isEdit && !password) {
      setPasswordError('Password is required')
      valid = false
    } else if (password && password.length < 6) {
      setPasswordError('Password must be at least 6 characters')
      valid = false
    } else {
      setPasswordError('')
    }

    return valid
  }

  const saveUser = useApiMutation(
    (data: { id?: number; name: string; email: string; status: string; password?: string }) =>
      data.id
        ? api.users.updateUser(data.id, {
            name: data.name,
            email: data.email,
            status: data.status,
            ...(data.password ? { password: data.password } : {}),
          })
        : api.users.createUser({
            name: data.name,
            email: data.email,
            password: data.password!,
            status: data.status,
          }),
    { invalidate: [qk.users] },
  )

  const handleSubmit = async (e: React.SyntheticEvent) => {
    e.preventDefault()

    if (!validate()) return

    setSaving(true)

    try {
      await saveUser.mutateAsync({
        id: isEdit ? Number(userId) : undefined,
        name: name.trim(),
        email: email.trim(),
        status,
        ...(password ? { password } : {}),
      })
      toast.success(isEdit ? 'User updated' : 'User created', {
        description: `User "${name.trim()}" has been ${isEdit ? 'updated' : 'created'}.`,
      })
      onSaved?.()
    } catch (err) {
      toast.error(isEdit ? 'Failed to update user' : 'Failed to create user', { description: String(err) })
    } finally {
      setSaving(false)
    }
  }

  if (isLoading) {
    return (
      <div className="animate-pulse space-y-4">
        <div className="h-10 w-1/3 rounded-lg bg-surface-200" />
        <div className="h-10 w-1/2 rounded-lg bg-surface-200" />
        <div className="h-10 w-1/4 rounded-lg bg-surface-200" />
      </div>
    )
  }

  if (fetchError) {
    return (
      <Card>
        <CardContent className="p-6 text-center">
          <h3 className="text-error font-medium">Failed to load user</h3>
          <p className="text-surface-500 text-sm mt-1">{fetchError.message}</p>
          <Button variant="outline" size="sm" className="mt-3" onClick={() => onCancel?.()}>
            Back to Users
          </Button>
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="max-w-lg">
      <Card>
        <CardHeader>
          <CardTitle>{isEdit ? 'Edit User' : 'Create User'}</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Name <span className="text-error">*</span>
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => {
                  setName(e.target.value)
                  setNameError('')
                }}
                placeholder="John Doe"
                className={`w-full rounded-lg border px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:outline-none focus:ring-1 ${
                  nameError
                    ? 'border-error focus:border-error focus:ring-error'
                    : 'border-surface-300 focus:border-brand-500 focus:ring-brand-500'
                }`}
              />
              {nameError && <p className="mt-1 text-xs text-error">{nameError}</p>}
            </div>

            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Email <span className="text-error">*</span>
              </label>
              <input
                type="email"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value)
                  setEmailError('')
                }}
                placeholder="john@example.com"
                className={`w-full rounded-lg border px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:outline-none focus:ring-1 ${
                  emailError
                    ? 'border-error focus:border-error focus:ring-error'
                    : 'border-surface-300 focus:border-brand-500 focus:ring-brand-500'
                }`}
              />
              {emailError && <p className="mt-1 text-xs text-error">{emailError}</p>}
            </div>

            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                {isEdit ? 'New Password (optional)' : 'Password'}
                {!isEdit && <span className="text-error">*</span>}
              </label>
              <input
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value)
                  setPasswordError('')
                }}
                placeholder={isEdit ? 'Leave empty to keep current' : 'Min. 6 characters'}
                className={`w-full rounded-lg border px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:outline-none focus:ring-1 ${
                  passwordError
                    ? 'border-error focus:border-error focus:ring-error'
                    : 'border-surface-300 focus:border-brand-500 focus:ring-brand-500'
                }`}
              />
              {passwordError && <p className="mt-1 text-xs text-error">{passwordError}</p>}
            </div>

            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Status</label>
              <select
                value={status}
                onChange={(e) => setStatus(e.target.value)}
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              >
                <option value="active">Active</option>
                <option value="suspended">Suspended</option>
                <option value="disabled">Disabled</option>
              </select>
            </div>

            <div className="flex items-center gap-3 pt-2">
              <Button type="submit" disabled={saving}>
                {saving ? 'Saving...' : isEdit ? 'Update User' : 'Create User'}
              </Button>
              <Button type="button" variant="outline" onClick={() => onCancel?.()}>
                Cancel
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

export function UserForm({ userId, onSaved, onCancel }: UserFormProps) {
  return (
    <QueryProvider>
      <UserFormContent userId={userId} onSaved={onSaved} onCancel={onCancel} />
    </QueryProvider>
  )
}
