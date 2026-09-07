\set ON_ERROR_STOP on
-- Only for a fresh disposable PostgreSQL container. GoTrue owns its migrations.
create role anon nologin;
create role authenticated nologin;
create role service_role nologin;
create role supabase_auth_admin login noinherit password 'local-auth-db-password';
create schema auth authorization supabase_auth_admin;
alter role supabase_auth_admin set search_path to auth;
grant create on database postgres to supabase_auth_admin;
