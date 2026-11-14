// Source ownership matters: a device task advertised by the cloud is metadata,
// not a conversation on this device, even when its project identifier matches.
export function buildSidebar({ projects = [], conversations = [], cloudProjects = [], cloudConversations = [], query = '' }) {
  const localIDs = new Set(conversations.map(item => item.id));
  const tasks = [
    ...conversations.map(item => ({ ...item, _source: 'local' })),
    ...cloudConversations.filter(item => !localIDs.has(item.id)).map(item => ({
      ...item, _source: item.source === 'local' ? 'remote' : 'cloud',
    })),
  ].sort((a, b) => timestamp(b.updated_at) - timestamp(a.updated_at) || String(a.id).localeCompare(String(b.id)));
  const needle = query.trim().toLocaleLowerCase();
  const matches = value => String(value || '').toLocaleLowerCase().includes(needle);
  const matchesTask = task => !needle || matches(task.title) || matches(task.project_name);
  const assigned = new Set();
  const groups = [];
  for (const project of [
    ...projects.map(item => ({ ...item, _source: 'local' })),
    ...cloudProjects.map(item => ({ ...item, _source: 'cloud' })),
  ]) {
    const members = tasks.filter(task => task.project_id === project.id && task._source === project._source);
    for (const task of members) assigned.add(task);
    const filtered = needle && !matches(project.name) ? members.filter(matchesTask) : members;
    if (!needle || matches(project.name) || filtered.length) {
      groups.push({ key: `${project._source}:${project.id}`, project, conversations: filtered, total: members.length });
    }
  }
  return { groups, ungrouped: tasks.filter(task => !assigned.has(task) && matchesTask(task)), total: tasks.length, searching: Boolean(needle) };
}

function timestamp(value) {
  return Date.parse(value) || 0;
}
