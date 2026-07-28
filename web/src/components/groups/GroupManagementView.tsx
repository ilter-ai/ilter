import { ManagementViewLayout } from '@/components/ui/ManagementViewLayout'
import { usePathNavigation } from '@/lib/usePathNavigation'
import { GroupForm } from './GroupForm'
import { GroupList } from './GroupList'
import { GroupMembers } from './GroupMembers'

export function GroupManagementView() {
  const { path, navigate } = usePathNavigation()

  const editMatch = path.match(/^\/groups\/([^/]+?)\/edit\/?$/)
  const isNew = path === '/groups/new' || path === '/groups/new/'

  if (isNew) {
    return (
      <ManagementViewLayout title="Create Group" onBack={() => navigate('/groups')}>
        <GroupForm mode="create" onSaved={() => navigate('/groups')} onCancel={() => navigate('/groups')} />
      </ManagementViewLayout>
    )
  }

  if (editMatch) {
    const groupId = Number(editMatch[1])
    return (
      <ManagementViewLayout title="Edit Group" onBack={() => navigate('/groups')}>
        <GroupForm
          mode="edit"
          groupId={groupId}
          onSaved={() => navigate('/groups')}
          onCancel={() => navigate('/groups')}
        />
        <GroupMembers groupId={groupId} />
      </ManagementViewLayout>
    )
  }

  return <GroupList onNavigate={navigate} />
}
