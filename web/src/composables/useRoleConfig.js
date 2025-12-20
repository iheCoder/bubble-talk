import { computed } from 'vue'

const ROLE_CONFIG = {
  '经济': {
    id: 'economist',
    name: '经济学家',
    tag: '机会成本',
    color: 'rgba(188, 214, 255, 0.7)',
    accent: 'rgba(140, 200, 255, 0.35)',
    avatar: 'E',
  },
  '心理': {
    id: 'psychologist',
    name: '心理咨询师',
    tag: '认知重评',
    color: 'rgba(255, 168, 209, 0.7)',
    accent: 'rgba(255, 168, 209, 0.35)',
    avatar: 'P',
  },
  '学习': {
    id: 'coach',
    name: '学习教练',
    tag: '元认知',
    color: 'rgba(124, 255, 219, 0.7)',
    accent: 'rgba(124, 255, 219, 0.35)',
    avatar: 'C',
  },
  '行为': {
    id: 'behaviorist',
    name: '行为学家',
    tag: '行为设计',
    color: 'rgba(255, 196, 110, 0.7)',
    accent: 'rgba(255, 196, 110, 0.35)',
    avatar: 'B',
  },
  '效率': {
    id: 'pm',
    name: '产品经理',
    tag: '系统思维',
    color: 'rgba(118, 245, 169, 0.7)',
    accent: 'rgba(118, 245, 169, 0.35)',
    avatar: 'PM',
  },
  '沟通': {
    id: 'mediator',
    name: '沟通专家',
    tag: '非暴力沟通',
    color: 'rgba(255, 212, 148, 0.7)',
    accent: 'rgba(255, 212, 148, 0.35)',
    avatar: 'M',
  },
  'default': {
    id: 'expert',
    name: '领域专家',
    tag: '知识向导',
    color: 'rgba(188, 214, 255, 0.7)',
    accent: 'rgba(140, 200, 255, 0.35)',
    avatar: 'X',
  }
}

const HOST_ROLE = {
  id: 'host',
  name: '主持人',
  tag: '引导者',
  color: 'rgba(124, 255, 219, 0.7)',
  accent: 'rgba(124, 255, 219, 0.35)',
  avatar: 'H',
}

const USER_ROLE = {
  id: 'user',
  name: '你',
  tag: '学习者',
  color: 'rgba(255, 199, 140, 0.8)',
  accent: 'rgba(255, 199, 140, 0.35)',
  avatar: '你',
}

export function useRoleConfig(bubbleTag) {
  const expertRole = computed(() => {
    return ROLE_CONFIG[bubbleTag.value] || ROLE_CONFIG['default']
  })

  const roles = computed(() => [HOST_ROLE, expertRole.value, USER_ROLE])

  const roleMap = computed(() => {
    return roles.value.reduce((acc, role) => {
      acc[role.id] = role
      return acc
    }, {})
  })

  return {
    expertRole,
    roles,
    roleMap,
    hostRole: HOST_ROLE,
    userRole: USER_ROLE,
  }
}


