import { ManagementViewLayout } from '@/components/ui/ManagementViewLayout'
import { usePathNavigation } from '@/lib/usePathNavigation'
import { UserForm } from './UserForm'
import { UserList } from './UserList'

export function UserManagementView() {
  const { path, navigate } = usePathNavigation()

  const editMatch = path.match(/^\/users\/([^/]+?)\/edit\/?$/)
  const isNew = path === '/users/new' || path === '/users/new/'

  if (isNew) {
    return (
      <ManagementViewLayout title="Create User" onBack={() => navigate('/users')}>
        <UserForm onSaved={() => navigate('/users')} onCancel={() => navigate('/users')} />
      </ManagementViewLayout>
    )
  }

  if (editMatch) {
    const userId = editMatch[1]
    return (
      <ManagementViewLayout title="Edit User" onBack={() => navigate('/users')}>
        <UserForm userId={userId} onSaved={() => navigate('/users')} onCancel={() => navigate('/users')} />
      </ManagementViewLayout>
    )
  }

  return <UserList onNavigate={navigate} />
}
