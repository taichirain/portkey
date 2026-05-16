import React from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';
import { SystemRoles } from '../types';

interface PermissionGuardProps {
  children: React.ReactNode;
  fallback?: React.ReactNode;
  redirectTo?: string;
}

interface RoleGuardProps extends PermissionGuardProps {
  role: string;
}

interface RolesGuardProps extends PermissionGuardProps {
  roles: string[];
  requireAll?: boolean;
}

interface PermissionGuardPermissionsProps extends PermissionGuardProps {
  permission: string;
}

interface PermissionsGuardProps extends PermissionGuardProps {
  permissions: string[];
  requireAll?: boolean;
}

export const RequireRole: React.FC<RoleGuardProps> = ({
  children,
  role,
  fallback = null,
  redirectTo,
}) => {
  const { hasRole, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  if (!hasRole(role)) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  return <>{children}</>;
};

export const RequireAnyRole: React.FC<RolesGuardProps> = ({
  children,
  roles,
  fallback = null,
  redirectTo,
}) => {
  const { hasAnyRole, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  if (!hasAnyRole(roles)) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  return <>{children}</>;
};

export const RequireAllRoles: React.FC<RolesGuardProps> = ({
  children,
  roles,
  fallback = null,
  redirectTo,
}) => {
  const { hasAllRoles, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  if (!hasAllRoles(roles)) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  return <>{children}</>;
};

export const RequirePermission: React.FC<PermissionGuardPermissionsProps> = ({
  children,
  permission,
  fallback = null,
  redirectTo,
}) => {
  const { hasPermission, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  if (!hasPermission(permission)) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  return <>{children}</>;
};

export const RequireAnyPermission: React.FC<PermissionsGuardProps> = ({
  children,
  permissions,
  fallback = null,
  redirectTo,
}) => {
  const { hasAnyPermission, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  if (!hasAnyPermission(permissions)) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  return <>{children}</>;
};

export const RequireAllPermissions: React.FC<PermissionsGuardProps> = ({
  children,
  permissions,
  fallback = null,
  redirectTo,
}) => {
  const { hasAllPermissions, isAuthenticated } = useAuth();

  if (!isAuthenticated) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  if (!hasAllPermissions(permissions)) {
    if (redirectTo) {
      return <Navigate to={redirectTo} replace />;
    }
    return <>{fallback}</>;
  }

  return <>{children}</>;
};

export const RequireSuperAdmin: React.FC<PermissionGuardProps> = ({
  children,
  fallback = null,
  redirectTo,
}) => {
  return (
    <RequireRole role={SystemRoles.SUPER_ADMIN} fallback={fallback} redirectTo={redirectTo}>
      {children}
    </RequireRole>
  );
};

export const RequireTenantAdmin: React.FC<PermissionGuardProps> = ({
  children,
  fallback = null,
  redirectTo,
}) => {
  return (
    <RequireAnyRole
      roles={[SystemRoles.SUPER_ADMIN, SystemRoles.TENANT_ADMIN]}
      fallback={fallback}
      redirectTo={redirectTo}
    >
      {children}
    </RequireAnyRole>
  );
};
