for i in role-binding.yaml role.yaml service-account.yaml
do 
  kubectl delete -f $i
done
